package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

func writeProcSys(path, value string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	n, err := f.Write([]byte(value))
	if err == nil && n < len(value) {
		err = io.ErrShortWrite
	}
	if err1 := f.Close(); err == nil {
		err = err1
	}
	return err
}

func vethNameForWorkload(namespace, podname string) string {
	// A SHA1 is always 20 bytes long, and so is sufficient for generating the
	// veth name and mac addr.
	//namespace = fmt.Sprintf("%s/%d", namespace, rand.Int())
	//podname = fmt.Sprintf("%s/%d", podname, rand.Int())
	h := sha1.New()
	h.Write([]byte(fmt.Sprintf("%s.%s", namespace, podname)))
	return fmt.Sprintf("%s%s", "veth", hex.EncodeToString(h.Sum(nil))[:11])
}

func Exec(cmd string, args ...string) error {
	return exec.Command(cmd, args...).Run()
}

func parseArgs(args string) *cniArgs {
	m := make(map[string]string)
	attrs := strings.Split(args, ";")
	for _, attr := range attrs {
		kv := strings.Split(attr, "=")
        if len(kv)!= 2 {
            continue
        }
        m[kv[0]] = kv[1]
	}
	return &cniArgs{
		namespace: m["K8s_POD_NAMESPACE"],
		podName:   m["K8s_POD_NAME"],
        containerID: m["K8s_POD_INFRA_CONTAINER_ID"],
	}
}