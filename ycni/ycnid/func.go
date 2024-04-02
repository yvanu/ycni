package main

import (
	"github.com/vishvananda/netlink"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"net"
	"syscall"
)

func addFunc(vxlanDevice *netlink.Vxlan) func(obj interface{}) {
	return func(obj interface{}) {
		n := obj.(*v1.Node)
		klog.Infof("node add event: %s", n.Name)
		_, ipnet, err := net.ParseCIDR(n.Spec.PodCIDR)
		if err != nil {
			klog.Fatalf("net.ParseCIDR(n.Spec.PodCIDR)失败: %s", err.Error())
		}
		vtepMacStr := n.Annotations[ycniVtepMacAnnotationKey]
		if vtepMacStr == "" {
			klog.Fatalf("node.Annotations[ycniVtepMacAnnotationKey]为空")
		}
		vtepMac, err := net.ParseMAC(vtepMacStr)
		if err != nil {
			klog.Fatalf("net.ParseMAC(vtepMacStr)失败: %s", err.Error())
		}
		hostIpStr := n.Annotations[ycniHostIPAnnotationKey]
		if hostIpStr == "" {
			klog.Fatalf("node.Annotations[ycniHostIPAnnotationKey]为空")
		}
		hostIp := net.ParseIP(hostIpStr)
		if hostIp == nil {
			klog.Fatalf("net.ParseIP(hostIpStr)失败: %s", err.Error())
		}
		// 添加arp记录
		err = netlink.NeighSet(&netlink.Neigh{
			LinkIndex:    vxlanDevice.Index,
			State:        netlink.NUD_PERMANENT, // 永久有效
			Type:         syscall.RTN_UNICAST,   // 单播
			IP:           ipnet.IP,
			HardwareAddr: vtepMac,
		})
		if err != nil {
			klog.Fatalf("netlink.NeighSet(&netlink.Neigh{LinkIndex: %d, State: %d, IP: %s, HardwareAddr: %s})失败: %s", vxlanDevice.Index, netlink.NUD_PERMANENT, hostIp, vtepMac, err.Error())
		}
		klog.Infof("添加arp记录成功")
		// 添加fdb记录
		err = netlink.NeighSet(&netlink.Neigh{
			LinkIndex:    vxlanDevice.Index,
			Family:       syscall.AF_BRIDGE,
			State:        netlink.NUD_PERMANENT,
			Flags:        netlink.NTF_SELF, // 表示要订阅变更事件
			IP:           hostIp,
			HardwareAddr: vtepMac,
		})
		if err != nil {
			klog.Fatalf("netlink.NeighSet(&netlink.Neigh{LinkIndex: %d, State: %d, IP: %s, HardwareAddr: %s})失败: %s", vxlanDevice.Index, netlink.NUD_PERMANENT, hostIp, vtepMac, err.Error())
		}
		err = netlink.RouteReplace(&netlink.Route{
			LinkIndex: vxlanDevice.Index,
			Scope:     netlink.SCOPE_UNIVERSE,
			Dst:       ipnet,
			Gw:        ipnet.IP,
			Flags:     syscall.RTNH_F_ONLINK,
		})
		if err != nil {
			klog.Fatalf("netlink.RouteReplace(&netlink.Route{LinkIndex: %d, Scope: %d, Dst: %s, Gw: %s, Flags: %d})失败: %s", vxlanDevice.Index, netlink.SCOPE_UNIVERSE, ipnet, ipnet.IP, syscall.RTNH_F_ONLINK, err.Error())
		}
		klog.Infof("添加路由表成功")
	}
}

func delFunc(vxlanDevice *netlink.Vxlan) func(obj interface{}) {
	return func(obj interface{}) {
		n := obj.(*v1.Node)
		klog.Infof("node del event: %s", n.Name)
		_, ipnet, err := net.ParseCIDR(n.Spec.PodCIDR)
		if err != nil {
			klog.Fatalf("net.ParseCIDR(n.Spec.PodCIDR)失败: %s", err.Error())
		}
		vtepMacStr := n.Annotations[ycniVtepMacAnnotationKey]
		if vtepMacStr == "" {
			klog.Fatalf("node.Annotations[ycniVtepMacAnnotationKey]为空")
		}
		vtepMac, err := net.ParseMAC(vtepMacStr)
		if err != nil {
			klog.Fatalf("net.ParseMAC(vtepMacStr)失败: %s", err.Error())
		}
		hostIpStr := n.Annotations[ycniHostIPAnnotationKey]
		if hostIpStr == "" {
			klog.Fatalf("node.Annotations[ycniHostIPAnnotationKey]为空")
		}
		hostIp := net.ParseIP(hostIpStr)
		if hostIp == nil {
			klog.Fatalf("net.ParseIP(hostIpStr)失败: %s", err.Error())
		}
		// 删除arp
		err = netlink.NeighDel(&netlink.Neigh{
			LinkIndex:    vxlanDevice.Index,
			State:        netlink.NUD_PERMANENT, // 永久有效
			Type:         syscall.RTN_UNICAST,   // 单播
			IP:           hostIp,
			HardwareAddr: vtepMac,
		})
		if err != nil {
			klog.Fatalf("netlink.NeighDel(&netlink.Neigh{LinkIndex: %d, State: %d, IP: %s, HardwareAddr: %s})失败: %s", vxlanDevice.Index, netlink.NUD_PERMANENT, hostIp, vtepMac, err.Error())
		}
		klog.Infof("删除arp记录成功")
		// 删除fdb记录
		err = netlink.NeighDel(&netlink.Neigh{
			LinkIndex:    vxlanDevice.Index,
			Family:       syscall.AF_BRIDGE,
			State:        netlink.NUD_PERMANENT,
			Flags:        netlink.NTF_SELF, // 表示要订阅变更事件
			IP:           hostIp,
			HardwareAddr: vtepMac,
		})
		if err != nil {
			klog.Fatalf("netlink.NeighDel(&netlink.Neigh{LinkIndex: %d, State: %d, IP: %s, HardwareAddr: %s})失败: %s", vxlanDevice.Index, netlink.NUD_PERMANENT, hostIp, vtepMac, err.Error())
		}
		err = netlink.RouteDel(&netlink.Route{
			LinkIndex: vxlanDevice.Index,
			Scope:     netlink.SCOPE_UNIVERSE,
			Dst:       ipnet,
			Gw:        ipnet.IP,
			Flags:     syscall.RTNH_F_ONLINK,
		})
		if err != nil {
			klog.Fatalf("netlink.RouteDel(&netlink.Route{LinkIndex: %d, Scope: %d, Dst: %s, Gw: %s, Flags: %d})失败: %s", vxlanDevice.Index, netlink.SCOPE_UNIVERSE, ipnet, ipnet.IP, syscall.RTNH_F_ONLINK, err.Error())
		}
		klog.Infof("删除路由表成功")
	}
}

func updateFunc(vxlanDevice *netlink.Vxlan) func(oldObj, newObj interface{}) {
	return func(oldObj, newObj interface{}) {
		oldNode := oldObj.(*v1.Node)
		newNode := newObj.(*v1.Node)
		if oldNode.Annotations[ycniVtepMacAnnotationKey] == newNode.Annotations[ycniVtepMacAnnotationKey] {
			return
		}
		klog.Infof("node 更新事件: %s", newNode.Name)
		n := newNode
		klog.Infof("node add event: %s", n.Name)
		_, ipnet, err := net.ParseCIDR(n.Spec.PodCIDR)
		if err != nil {
			klog.Fatalf("net.ParseCIDR(n.Spec.PodCIDR)失败: %s", err.Error())
		}
		vtepMacStr := n.Annotations[ycniVtepMacAnnotationKey]
		if vtepMacStr == "" {
			klog.Fatalf("node.Annotations[ycniVtepMacAnnotationKey]为空")
		}
		vtepMac, err := net.ParseMAC(vtepMacStr)
		if err != nil {
			klog.Fatalf("net.ParseMAC(vtepMacStr)失败: %s", err.Error())
		}
		hostIpStr := n.Annotations[ycniHostIPAnnotationKey]
		if hostIpStr == "" {
			klog.Fatalf("node.Annotations[ycniHostIPAnnotationKey]为空")
		}
		hostIp := net.ParseIP(hostIpStr)
		if hostIp == nil {
			klog.Fatalf("net.ParseIP(hostIpStr)失败: %s", err.Error())
		}
		// 添加arp记录
		err = netlink.NeighSet(&netlink.Neigh{
			LinkIndex:    vxlanDevice.Index,
			State:        netlink.NUD_PERMANENT, // 永久有效
			Type:         syscall.RTN_UNICAST,   // 单播
			IP:           hostIp,
			HardwareAddr: vtepMac,
		})
		if err != nil {
			klog.Fatalf("netlink.NeighSet(&netlink.Neigh{LinkIndex: %d, State: %d, IP: %s, HardwareAddr: %s})失败: %s", vxlanDevice.Index, netlink.NUD_PERMANENT, hostIp, vtepMac, err.Error())
		}
		klog.Infof("添加arp记录成功")
		// 添加fdb记录
		err = netlink.NeighSet(&netlink.Neigh{
			LinkIndex:    vxlanDevice.Index,
			Family:       syscall.AF_BRIDGE,
			State:        netlink.NUD_PERMANENT,
			Flags:        netlink.NTF_SELF, // 表示要订阅变更事件
			IP:           hostIp,
			HardwareAddr: vtepMac,
		})
		if err != nil {
			klog.Fatalf("netlink.NeighSet(&netlink.Neigh{LinkIndex: %d, State: %d, IP: %s, HardwareAddr: %s})失败: %s", vxlanDevice.Index, netlink.NUD_PERMANENT, hostIp, vtepMac, err.Error())
		}
		err = netlink.RouteReplace(&netlink.Route{
			LinkIndex: vxlanDevice.Index,
			Scope:     netlink.SCOPE_UNIVERSE,
			Dst:       ipnet,
			Gw:        ipnet.IP,
			Flags:     syscall.RTNH_F_ONLINK,
		})
		if err != nil {
			klog.Fatalf("netlink.RouteReplace(&netlink.Route{LinkIndex: %d, Scope: %d, Dst: %s, Gw: %s, Flags: %d})失败: %s", vxlanDevice.Index, netlink.SCOPE_UNIVERSE, ipnet, ipnet.IP, syscall.RTNH_F_ONLINK, err.Error())
		}
		klog.Infof("添加路由表成功")
	}
}
