package nacos

import (
	"net"
	"net/http"
)

func GetLocalIP() string {
	intaddrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, i := range intaddrs {
			if addr, ok := i.(*net.IPNet); ok && !addr.IP.IsLoopback() {
				if addr.IP.To4() != nil {
					return addr.IP.String()
				}
			}
		}
	}
	return "127.0.0.1"
}

func decode(httpResponse *http.Response, out interface{}) error {
	dec := json.NewDecoder(httpResponse.Body)
	defer httpResponse.Body.Close()
	return dec.Decode(out)
}

func encode(data interface{}) string {
	b, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return string(b)
}
