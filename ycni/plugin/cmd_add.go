package main

import (
	"encoding/json"
	"fmt"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	types100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/plugins/ipam/host-local/backend/allocator"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
	"net"
	"ycni/log"
)

var (
	defaultOutInterface   = "eth0"
	defaultHostVethMac, _ = net.ParseMAC("EE:EE:EE:EE:EE:EE")
	defaultPodGw          = net.IPv4(169, 254, 1, 1)
	defaultGwIPNet        = &net.IPNet{IP: defaultPodGw, Mask: net.CIDRMask(32, 32)}
	_, IPv4AllNet, _      = net.ParseCIDR("0.0.0.0/0")
	defaultRoutes         = []*net.IPNet{IPv4AllNet}
)

type IPAM struct {
	Type       string `json:"type"`
	Subnet     string `json:"subnet"`
	RangeStart string `json:"rangeStart"`
	RangeEnd   string `json:"rangeEnd"`
}

type YCNIConfig struct {
	CNIVersion string `json:"cniVersion"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	IPAM       IPAM   `json:"ipam"`
}

type cniArgs struct {
	namespace   string
	podName     string
	containerID string
}

func cmdAdd(args *skel.CmdArgs) error {
	log.Debugf("cmdAdd containerID: %s", args.ContainerID)
	log.Debugf("cmdAdd netNs: %s", args.Netns)
	log.Debugf("cmdAdd ifName: %s", args.IfName)
	log.Debugf("cmdAdd args: %s", args.Args)
	log.Debugf("cmdAdd path: %s", args.Path)
	log.Debugf("cmdAdd stdin: %s", string(args.StdinData))

	/*
		cmdAdd containerID: cnitool-9d3dec7303f5bdb39c17
		cmdAdd netNs: /var/run/netns/demonet
		cmdAdd ifName: eth0
		cmdAdd args:
		cmdAdd path: /opt/cni/bin
		cmdAdd stdin: {"cniVersion":"0.3.1","ipam":{"subnet":"10.244.0.0/24","type":"host-local"},"name":"ycni0","type":"ycni"}
	*/
	var err error
	var ycniConf YCNIConfig
	err = json.Unmarshal(args.StdinData, &ycniConf)
	if err != nil {
		log.Debugf("加载cni配置文件错误: %s", err.Error())
		return errors.Wrap(err, "加载cni配置文件错误")
	}

	// 解析args  todo
	/*
		type cniArgs struct {
			namespace   string
			podName     string
			containerID string
		}
	*/
	cniargs := parseArgs(args.Args)

	// 给ns加上ip  利用ipam插件分配ip
	// parse子网
	ipNet, err := types.ParseCIDR(ycniConf.IPAM.Subnet)
	if err != nil {
		return errors.Wrap(err, "parse子网失败")
	}
	var startIp, endIp net.IP
	if ycniConf.IPAM.RangeStart != "" {
		startIp = net.ParseIP(ycniConf.IPAM.RangeStart)
		if startIp == nil {
			return errors.Wrap(err, "获取起始ip失败")
		}
	}
	if ycniConf.IPAM.RangeEnd != "" {
		endIp = net.ParseIP(ycniConf.IPAM.RangeEnd)
		if endIp == nil {
			return errors.Wrap(err, "获取结束ip失败")
		}
	}
	// 获取ipam配置传给ipam插件
	ipamConf := allocator.Net{
		Name:       ycniConf.Name,
		CNIVersion: ycniConf.CNIVersion,
		IPAM: &allocator.IPAMConfig{
			Type: ycniConf.IPAM.Type,
			Ranges: []allocator.RangeSet{
				{
					{
						Subnet:     types.IPNet(*ipNet),
						RangeStart: startIp,
						RangeEnd:   endIp,
					},
				},
			},
		},
	}
	ipamConfBytes, err := json.Marshal(ipamConf)
	if err != nil {
		return errors.Wrap(err, "获取ipam配置失败")
	}
	log.Debugf("ipam配置：%s", string(ipamConfBytes))
	ipamResult, err := ipam.ExecAdd(ycniConf.IPAM.Type, ipamConfBytes)
	if err != nil {
		log.Debugf("分配ip失败: %s", err.Error())
		return errors.Wrap(err, "给ns分配ip失败")
	}
	// 获取具体的ipam result
	result, err := types100.GetResult(ipamResult)
	if err != nil {
		log.Debugf("转换ipam result失败: %s", err.Error())
		return errors.Wrap(err, "转化ipam result失败")
	}

	// 随机生成veth name
	hostVethName := vethNameForWorkload(cniargs.namespace, cniargs.namespace)
	log.Debugf("hostVethName: %s", hostVethName)

	// 配置 veth pair
	// 如果老的已存在则删除
	oldHostVeth, err := netlink.LinkByName(hostVethName)
	if err == nil {
		// 说明已存在
		err = netlink.LinkDel(oldHostVeth)
		if err != nil {
			return errors.Wrapf(err, "删除old hostveth失败: %v", hostVethName)
		}
	}

	var hasIpv4 bool
	ns.WithNetNSPath(args.Netns, func(netNS ns.NetNS) error {
		// 下面是要在容器中创建的veth
		veth := &netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name: args.IfName,
				MTU:  1500,
			},
			PeerName: hostVethName,
		}

		if err := netlink.LinkAdd(veth); err != nil {
			return errors.Wrapf(err, "在ns中创建veth失败")
		}

		hostVeth, err := netlink.LinkByName(hostVethName)
		if err != nil {
			return errors.Wrapf(err, "没找到对应的veth: %s", hostVethName)
		}

		if err := netlink.LinkSetHardwareAddr(hostVeth, defaultHostVethMac); err != nil {
			log.Debugf("failed to Set MAC of %q: %v. Using kernel generated MAC.", hostVethName, err)
		}

		for _, addr := range result.IPs {
			if addr.Address.IP.To4() != nil {
				hasIpv4 = true
				addr.Address.Mask = net.CIDRMask(32, 32)
			}
		}

		// up 宿主机上的veth
		if err = netlink.LinkSetUp(hostVeth); err != nil {
			return errors.Wrapf(err, "up 宿主机上的veth: %s失败", hostVeth)
		}

		nsVeth, err := netlink.LinkByName(args.IfName)
		if err != nil {
			return errors.Wrapf(err, "没找到ns内的veth: %s", args.IfName)
		}
		// up 容器内的veth
		if err = netlink.LinkSetUp(nsVeth); err != nil {
			return errors.Wrapf(err, "up 容器上上的veth: %s失败", nsVeth)
		}

		if hasIpv4 {
			// 添加路由 169.254.1.1 dev eth0
			if err := netlink.RouteAdd(
				&netlink.Route{
					LinkIndex: nsVeth.Attrs().Index,
					Scope:     netlink.SCOPE_LINK,
					Dst:       defaultGwIPNet,
				},
			); err != nil {
				return errors.Wrap(err, "容器内添加路由失败")
			}

			// 添加默认路由 0.0.0.0/0 via 169.254.1.1 dev eth0
			for _, r := range defaultRoutes {
				if r.IP.To4() == nil {
					continue
				}
				if err = ip.AddRoute(r, defaultPodGw, nsVeth); err != nil {
					return errors.Wrap(err, "容器内添加默认路由失败")
				}
			}
		}

		for _, addr := range result.IPs {
			if err = netlink.AddrAdd(nsVeth, &netlink.Addr{IPNet: &addr.Address}); err != nil {
				return errors.Wrapf(err, "容器内veth配置ip失败")
			}
		}

		// 把hostVeth放入宿主机网络命名空间  需要重新up
		if err = netlink.LinkSetNsFd(hostVeth, int(netNS.Fd())); err != nil {
			return errors.Wrapf(err, "把hostveth放到宿主机失败")
		}
		return nil
	})

	// 设置arp代理
	if err = writeProcSys(fmt.Sprintf("/proc/sys/net/ipv4/conf/%s/proxy_arp", hostVethName), "1"); err != nil {
		log.Debugf("开启arp代理失败")
		return errors.Wrap(err, "开启arp代理失败")
	}

	// up hostVeth
	hostVeth, err := netlink.LinkByName(hostVethName)
	if err != nil {
		log.Debugf("没有找到hostVeth: %s", hostVethName)
		return errors.Wrapf(err, "没有找到hostVeth: %s", hostVethName)
	}
	if err = netlink.LinkSetUp(hostVeth); err != nil {
		log.Debugf("hostVeth up失败: %s", err.Error())
		return errors.Wrap(err, "hostVeth up失败")
	}

	// 配置iptables
	// 配置forward链
	Exec("iptables", "-A", "FORWARD", "--out-interface", defaultOutInterface, "--in-interface", hostVethName, "-j", "ACCEPT")
	Exec("iptables", "-A", "FORWARD", "--out-interface", hostVethName, "--in-interface", defaultOutInterface, "-j", "ACCEPT")
	// 设置postrouting链
	Exec("iptables", "-t", "nat", "-A", "POSTROUTING", "--source", ycniConf.IPAM.Subnet, "--out-interface", defaultOutInterface, "-j", "MASQUERADE")

	// 宿主机配置往容器方向的路由
	for _, ipaddr := range result.IPs {
		route := netlink.Route{
			LinkIndex: hostVeth.Attrs().Index,
			Scope:     netlink.SCOPE_LINK,
			Dst:       &ipaddr.Address,
		}
		if err := netlink.RouteAdd(&route); err != nil {
			log.Debugf("宿主机添加路由失败 %s", err.Error())
			return errors.Wrapf(err, "宿主机添加路由失败")
		}
	}

	result.Interfaces = append(result.Interfaces, &types100.Interface{
		Name: hostVethName},
	)

	for _, ip := range result.IPs {
		ip.Gateway = nil
	}

	if err = result.Print(); err != nil {
		log.Debugf("result Print error: %s", err.Error())
		return err
	}

	log.Debugf("cmdAdd success")
	return nil
}
