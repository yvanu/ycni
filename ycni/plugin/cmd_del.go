package main

import (
	"encoding/json"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/plugins/ipam/host-local/backend/allocator"
	"github.com/pkg/errors"
	"net"
	"ycni/log"
)

func cmdDel(args *skel.CmdArgs) error {
	log.Debugf("cmdDel containerID: %s", args.ContainerID)
	log.Debugf("cmdDel netNs: %s", args.Netns)
	log.Debugf("cmdDel ifName: %s", args.IfName)
	log.Debugf("cmdDel args: %s", args.Args)
	log.Debugf("cmdDel path: %s", args.Path)
	log.Debugf("cmdDel stdin: %s", string(args.StdinData))

	var err error
	var ycniConf YCNIConfig
	err = json.Unmarshal(args.StdinData, &ycniConf)
	if err != nil {
		log.Debugf("加载cni配置文件错误: %s", err.Error())
		return errors.Wrap(err, "加载cni配置文件错误")
	}

	log.Debugf("cmdDel conf: %+v", ycniConf)

	// 释放ip
	ipNet, err := types.ParseCIDR(ycniConf.IPAM.Subnet)
	if err != nil {
		return errors.Wrap(err, "parse子网失败")
	}
	var startIP, endIP net.IP
	if ycniConf.IPAM.RangeStart != "" {
		startIP = net.ParseIP(ycniConf.IPAM.RangeStart)
		if startIP == nil {
			return errors.Wrap(err, "解析start ip 失败")
		}
	}
	if ycniConf.IPAM.RangeEnd != "" {
		endIP = net.ParseIP(ycniConf.IPAM.RangeEnd)
		if endIP == nil {
			return errors.Wrap(err, "解析end ip失败")
		}
	}

	ipamConf := allocator.Net{
		Name:       ycniConf.Name,
		CNIVersion: ycniConf.CNIVersion,
		IPAM: &allocator.IPAMConfig{
			Type: ycniConf.IPAM.Type,
			Ranges: []allocator.RangeSet{
				{
					{
						Subnet:     types.IPNet(*ipNet),
						RangeStart: startIP,
						RangeEnd:   endIP,
					},
				},
			},
		},
	}
	ipamConfBytes, err := json.Marshal(ipamConf)
	if err != nil {
		return errors.Wrapf(err, "marshal ipam conf error")
	}
	log.Debugf("ipamConfBytes: %s", string(ipamConfBytes))

	err = ipam.ExecDel(ycniConf.IPAM.Type, ipamConfBytes)
	if err != nil {
		log.Debugf("释放ip失败")
		return errors.Wrap(err, "释放ip失败")
	}
	
	cniargs := parseArgs(args.Args)
	hostVethName := vethNameForWorkload(cniargs.namespace, cniargs.podName)
	log.Debugf("hostVethName: %s", hostVethName)
	// 删除veth pair
	if err = ip.DelLinkByName(hostVethName); err != nil {
		log.Debugf("删除veth失败")
		return errors.Wrap(err, "删除veth失败")
	}

	// 删除forward链
	Exec("iptables", "-D", "FORWARD", "--out-interface", defaultOutInterface, "--in-interface", hostVethName)
	Exec("iptables", "-D", "FORWARD", "--out-interface", hostVethName, "--in-interface", defaultOutInterface)
	// 设置postrouting链
	Exec("iptables", "-t", "nat", "-D", "POSTROUTING", "--source", ycniConf.IPAM.Subnet, "--out-interface", defaultOutInterface)

	log.Debugf("cmdDel: success")
	return nil
}
