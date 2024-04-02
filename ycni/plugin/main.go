package main

import (
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/utils/buildversion"
	"ycni/log"
)

const (
	defaultLogFile = "/var/log/ycni.log"
)

func main() {
	log.InitZapLog(defaultLogFile)
	skel.PluginMain(cmdAdd, nil, cmdDel, version.All, buildversion.BuildString("ycni"))
}
