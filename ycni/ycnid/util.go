package main

import (
	"crypto/rand"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
	"net"
	"strings"
	"syscall"
)

func newHardwareAddr() (net.HardwareAddr, error) {
	hardwareAddr := make(net.HardwareAddr, 6)
	if _, err := rand.Read(hardwareAddr); err != nil {
		return nil, errors.Wrap(err, "failed to read hardware address")
	}

	// 确保是单播以及是本地管理mac
	hardwareAddr[0] = (hardwareAddr[0] & 0xfe) | 0x02
	return hardwareAddr, nil
}

func getDefaultGatewayInterface() (*net.Interface, error) {

	routes, err := netlink.RouteList(nil, syscall.AF_INET)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get routes")
	}
	for _, route := range routes {
		if route.Dst == nil || route.Dst.String() == "0.0.0.0/0" {
			if route.LinkIndex <= 0 {
				return nil, errors.New("failed to get default gateway interface")
			}
			return net.InterfaceByIndex(route.LinkIndex)
		}
	}
	return nil, errors.New("failed to get default gateway interface")
}

func getInterfaceAddr(gateway *net.Interface) ([]netlink.Addr, error) {
	return netlink.AddrList(&netlink.Device{
		LinkAttrs: netlink.LinkAttrs{
			Index: gateway.Index,
		},
	}, syscall.AF_INET)
}

func ensureVxlan(vxlan *netlink.Vxlan) (*netlink.Vxlan, error) {
	link, err := netlink.LinkByName(vxlan.Name)
	if err == nil {
		v, ok := link.(*netlink.Vxlan)
		if !ok {
			return nil, errors.Errorf("link %s already exists but not vxlan device", vxlan.Name)
		}

		klog.Infof("vxlan device %s already exists", vxlan.Name)
		return v, nil
	}

	if !strings.Contains(err.Error(), "Link not found") {
		return nil, errors.Wrapf(err, "get link %s error", vxlan.Name)
	}

	klog.Infof("vxlan device %s not found, and create it", vxlan.Name)

	if err = netlink.LinkAdd(vxlan); err != nil {
		return nil, errors.Wrap(err, "LinkAdd error")
	}

	link, err = netlink.LinkByName(vxlan.Name)
	if err != nil {
		return nil, errors.Wrap(err, "LinkByName error")
	}

	return link.(*netlink.Vxlan), nil
}
