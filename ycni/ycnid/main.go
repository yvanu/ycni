package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
	v1 "k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v13 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"k8s.io/sample-controller/pkg/signals"
	"net"
	"os"
)

func main() {
	// 获取当前所在node
	stopChan := signals.SetupSignalHandler()
	cfg, err := clientcmd.BuildConfigFromFlags("", "/etc/kubernetes/kubelet.conf")
	if err != nil {
		klog.Fatalf("Failed to build config: %s", err.Error())
	}
	clientSet, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatal("Failed to create clientset")
	}
	factory := informers.NewSharedInformerFactory(clientSet, 0)
	nodeInformer := factory.Core().V1().Nodes().Informer()
	go nodeInformer.Run(stopChan)
	if !cache.WaitForCacheSync(stopChan, nodeInformer.HasSynced) {
		klog.Fatal("Failed to wait for caches to sync")
	}
	nodeLister := factory.Core().V1().Nodes().Lister()
	klog.Infof("初始化k8s链接成功")

	node, err := GetCurrentNode(clientSet, nodeLister)
	if err != nil {
		klog.Fatalf("获取当前node失败: %s", err.Error())
	}
	if node.Spec.PodCIDR == "" {
		klog.Fatalf("node: %s, node.Spec.PodCIDR为空", node.Name)
	}
	klog.Infof("获取node信息成功: %+v", node)
	// 初始化cni插件所需配置文件
	fd, err := os.OpenFile("/etc/cni/net.d/00-ycni.conf", os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.ModeAppend|os.ModePerm)
	if err != nil {
		klog.Fatalf("打开/etc/cni/net.d/00-ycni.conf失败: %s", err.Error())
	}
	defer fd.Close()
	_, err = fd.Write([]byte(fmt.Sprintf(cniConfTemplate, node.Spec.PodCIDR)))
	if err != nil {
		klog.Fatalf("写入/etc/cni/net.d/00-ycni.conf失败: %s", err.Error())
	}
	klog.Infof("初始化cni插件配置文件成功")
	// 初始化网络信息，这里用vxlan实现
	// 初始化vxlan
	vxlanDevice, err := InitVxlanDevice(node.Spec.PodCIDR)
	if err != nil {
		klog.Fatalf("初始化vxlan失败: %s", err.Error())
	}
	// 上传本机vxlan信息
	newNode := node.DeepCopy()

	newNode.Annotations[ycniHostIPAnnotationKey] = vxlanDevice.SrcAddr.String()
	newNode.Annotations[ycniVtepMacAnnotationKey] = vxlanDevice.HardwareAddr.String()

	oldNodeData, err := json.Marshal(node)
	if err != nil {
		klog.Fatalf("json.Marshal(node)失败: %s", err.Error())
	}
	newNodeData, err := json.Marshal(newNode)
	if err != nil {
		klog.Fatalf("json.Marshal(newNode)失败: %s", err.Error())
	}
	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldNodeData, newNodeData, v1.Node{})
	if err != nil {
		klog.Fatalf("strategicpatch.CreateTwoWayMergePatch(oldNodeData, newNodeData, v1.Node{})失败: %s", err.Error())
	}
	_, err = clientSet.CoreV1().Nodes().Patch(context.TODO(), node.Name, types.StrategicMergePatchType, patchBytes, v12.PatchOptions{})
	if err != nil {
		klog.Fatalf("clientSet.CoreV1().Nodes().Patch(context.TODO(), node.Name, types.StrategicMergePatchType, patchBytes, v12.PatchOptions{})失败: %s", err.Error())
	}
	klog.Infof("上传本机vxlan信息结束")
	// 启动控制器监控node信息
	nodeInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			n, ok := obj.(*v1.Node)
			if !ok {
				return false
			}
			// todo 需要调度要master
			return n.Name != node.Name
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    addFunc(vxlanDevice),
			DeleteFunc: delFunc(vxlanDevice),
			UpdateFunc: updateFunc(vxlanDevice),
		},
	})
	klog.Infof("启动ycni成功")
	<-stopChan
}

func InitVxlanDevice(cidr string) (*netlink.Vxlan, error) {
	hardwareAddr, err := newHardwareAddr()
	if err != nil {
		return nil, errors.Wrap(err, "随机生成mac 地址失败")
	}
	// 获取路由出口网卡
	gateway, err := getDefaultGatewayInterface()
	if err != nil {
		return nil, errors.Wrap(err, "获取路由出口网卡失败")
	}
	// 获取出口ip
	localHostAddrs, err := getInterfaceAddr(gateway)
	if err != nil {
		return nil, errors.Wrap(err, "获取出口ip失败")
	}
	if len(localHostAddrs) == 0 {
		return nil, errors.New("获取出口ip失败")
	}
	vxlan := &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:         vxlanName,
			HardwareAddr: hardwareAddr,
			MTU:          gateway.MTU - encapOverhead,
		},
		VxlanId:      vxlanVNI,
		SrcAddr:      localHostAddrs[0].IP,
		VtepDevIndex: gateway.Index,
		Port:         vxlanPort,
	}

	vxlan, err = ensureVxlan(vxlan)
	if err != nil {
		return nil, errors.Wrap(err, "创建vxlan失败")
	}

	// 给vxlan设备配置地址
	_, podCidr, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, errors.Wrap(err, "解析cidr失败")
	}
	existAddrs, err := netlink.AddrList(vxlan, netlink.FAMILY_V4)
	if err != nil {
		return nil, errors.Wrap(err, "获取vxlan地址失败")
	}
	if len(existAddrs) == 0 {
		// 配置ip
		if err = netlink.AddrAdd(vxlan, &netlink.Addr{
			IPNet: &net.IPNet{
				IP:   podCidr.IP,
				Mask: net.IPv4Mask(255, 255, 255, 255),
			},
		}); err != nil {
			return nil, errors.Wrap(err, "配置ip失败")
		}
	}
	// 启动设备
	if err = netlink.LinkSetUp(vxlan); err != nil {
		return nil, errors.Wrap(err, "启动设备失败")
	}
	return vxlan, nil
}

func GetCurrentNode(clientSet *kubernetes.Clientset, nodeLister v13.NodeLister) (*v1.Node, error) {
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		podName := os.Getenv("POD_NAME")
		podNameSpace := os.Getenv("POD_NAMESPACE")
		if podName == "" || podNameSpace == "" {
			return nil, errors.New("pod_name和pod_namespace必须设置")
		}
		pod, err := clientSet.CoreV1().Pods(podNameSpace).Get(context.TODO(), podName, v12.GetOptions{})
		if err != nil {
			return nil, errors.Wrapf(err, "获取pod信息失败: podName: %s, podNameSpace: %s", podName, podNameSpace)
		}
		nodeName = pod.Spec.NodeName
		if nodeName == "" {
			return nil, errors.New("从pod获取node name失败")
		}
	}
	node, err := nodeLister.Get(nodeName)
	if err != nil {
		return nil, errors.Wrapf(err, "获取node信息失败: nodeName: %s", nodeName)
	}
	return node, nil
}

var cniConfTemplate = `{
  "name": "ycni0",
  "cniVersion": "0.3.1",
  "type": "ycni",
  "ipam": {
    "type": "host-local",
    "subnet": "%s"
  }
}`
