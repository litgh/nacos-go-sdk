package nacos

import (
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var failoverSwitchFile = "00-00---000-VIPSRV_FAILOVER_SWITCH-000---00-00"

type failover struct {
	ns           *namingClient
	failoverDir  string
	failoverMode bool
	serviceMap   map[string]*ServiceInfo
}

func newFailover(ns *namingClient) *failover {
	f := &failover{
		ns: ns,
	}
	f.init()
	return f
}

func (f *failover) init() {
	f.failoverDir = path.Join(f.ns.c.config.CacheDir, "/failover")
	info, err := os.Stat(f.failoverDir)
	if os.IsNotExist(err) {
		os.MkdirAll(f.failoverDir, os.ModeDir)
	} else if !info.IsDir() {
		os.Remove(f.failoverDir)
		os.MkdirAll(f.failoverDir, os.ModeDir)
	}
	go func(f *failover) {
		t := time.NewTicker(time.Second * 5)
		for {
			select {
			case <-t.C:
				f.switchRefresher()
			case <-f.ns.c.ctx.Done():
				t.Stop()
				return
			}
		}
	}(f)

	go func(f *failover) {
		t := time.NewTicker(time.Hour * 24)
		firstDelay := time.NewTimer(time.Minute * 30)
		startUpDelay := time.NewTimer(time.Second * 10)
		for {
			select {
			case <-t.C:
				f.writeFile()
			case <-firstDelay.C:
				f.writeFile()
			case <-startUpDelay.C:
				files, err := ioutil.ReadDir(f.failoverDir)
				if err != nil {
					continue
				}
				if len(files) == 0 {
					f.writeFile()
				}
			case <-f.ns.c.ctx.Done():
				t.Stop()
				return
			}
		}
	}(f)
}

func (f *failover) isFailoverSwitch() bool {
	return f.failoverMode
}

func (f *failover) switchRefresher() {
	switchFile, err := os.Stat(path.Join(f.failoverDir, failoverSwitchFile))
	if os.IsNotExist(err) {
		f.failoverMode = false
		return
	}
	modified := switchFile.ModTime()
	var lastModified int64
	t := time.NewTicker(time.Second * 5)
	for {
		select {
		case <-f.ns.c.ctx.Done():
			return
		case <-t.C:
			if lastModified < modified.Unix() {
				lastModified = modified.Unix()
				b, _ := ioutil.ReadFile(path.Join(f.failoverDir, failoverSwitchFile))
				if string(b) == "" {
					f.failoverMode = false
				} else if string(b) == "1" {
					f.failoverMode = true
					f.readFile()
				} else {
					f.failoverMode = false
				}
			}
		}
	}
}

func (f *failover) readFile() {
	filepath.Walk(f.failoverDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || strings.HasSuffix(info.Name(), failoverSwitchFile) {
			return nil
		}
		b, _ := ioutil.ReadFile(path)
		var serviceInfo ServiceInfo
		err = json.Unmarshal(b, &serviceInfo)
		if err == nil {
			f.serviceMap[serviceInfo.GetKey()] = &serviceInfo
		}
		return nil
	})
}

func (f *failover) writeFile() {
	for _, v := range f.ns.serviceInfoMap {
		if v.GetKey() == "000--00-ALL_IPS--00--000" ||
			v.Name == "envList" ||
			v.Name == "00-00---000-ENV_CONFIGS-000---00-00" ||
			v.Name == "vipclient.properties" ||
			v.Name == "00-00---000-ALL_HOSTS-000---00-00" {
			continue
		}
		data := v.JsonFromServer
		if data == "" {
			data = encode(v)
		}
		ioutil.WriteFile(path.Join(f.failoverDir, url.PathEscape(v.GetKey())), []byte(data), 0666)
	}
}

func (f *failover) getService(key string) *ServiceInfo {
	return f.serviceMap[key]
}
