# ycni
cni开发心得
1. plugin
● plugin由kubelet调用，用于给pod创建veth-pair，分配ip等。
● 本项目采用开源ipam插件进行ip管理。
● plugin需实现如下函数：
func cmdAdd(args *skel.CmdArgs) error {}
func cmdDel(args *skel.CmdArgs) error {}
func cmdCheck(args *skel.CmdArgs) error {}
cmdAdd用于pod创建时给ns创建veth-pair，分配ip等。cmdDel是反操作。cmdCheck是非必须。
● plugin插件所使用的配置文件位于/etc/cni/net.d下，文件必须以conf或conflist结尾(json似乎也可以)，本项目使用的配置文件如下。如果/etc/cni/net.d下存在多个配置文件，以字母序文件名选择配置文件进行plugin调用。
{
  "name": "ycni0",
  "cniVersion": "0.3.1",
  "type": "ycni",
  "ipam": {
    "type": "host-local",
    "subnet": "10.244.0.0/24"
  }
}
● 总的说，plugin用来给ns插上网线。
2. ycnid
● ycnid是部署在集群上所有节点的daemonset，用于构建节点间的网络。
● daemon程序没有固定要求，能打通跨node间路由都可以。跨三层可以采用vxlan、tun/tap等技术方案，二层互通则可以直接使用host-gateway方案。本项目采用的是vxlan方式。
● vxlan通过mac in udp实现了三层互通
● 该daemon程序主要做了如下事情
  ○ 在本机创建vetp设备
  ○ 监控其他node信息，有新增node时添加arp记录，fdb记录，路由
  ○ 开启路由转发
