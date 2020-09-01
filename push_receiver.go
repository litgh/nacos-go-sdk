package nacos

/*
 * Copyright 1999-2020 Alibaba Group Holding Ltd.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
import (
	"bytes"
	"compress/gzip"
	"io/ioutil"
	"math/rand"
	"net"
	"strconv"
	"time"
)

type pushReceiver struct {
	port int
	host string
	ns   *namingClient
}

type pushData struct {
	PushType    string `json:"type"`
	Data        string `json:"data"`
	LastRefTime int64  `json:"lastRefTime"`
}

var (
	GZIP_MAGIC = []byte("\x1F\x8B")
)

func newPushRecevier(ns *namingClient) *pushReceiver {
	pr := pushReceiver{
		ns: ns,
	}
	go pr.startServer()
	return &pr
}

func (us *pushReceiver) tryListen() (*net.UDPConn, bool) {
	addr, err := net.ResolveUDPAddr("udp", us.host+":"+strconv.Itoa(us.port))
	if err != nil {
		us.ns.c.logger.Error("can't resolve address,err: %+v", err)
		return nil, false
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		us.ns.c.logger.Error("error listening %s:%d,err:%+v", us.host, us.port, err)
		return nil, false
	}

	return conn, true
}

func (us *pushReceiver) startServer() {
	var conn *net.UDPConn

	for i := 0; i < 3; i++ {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		port := r.Intn(1000) + 54951
		us.port = port
		conn1, ok := us.tryListen()

		if ok {
			conn = conn1
			us.ns.c.logger.Info("udp server start, port: " + strconv.Itoa(port))
			break
		}

		if !ok && i == 2 {
			us.ns.c.logger.Error("failed to start udp server after trying 3 times.")
		}
	}

	defer conn.Close()
	for {
		us.handleClient(conn)
	}
}

func (us *pushReceiver) handleClient(conn *net.UDPConn) {
	data := make([]byte, 4024)
	n, remoteAddr, err := conn.ReadFromUDP(data)
	if err != nil {
		us.ns.c.logger.Error("failed to read UDP msg because of %+v", err)
		return
	}

	s := us.tryDecompressData(data[:n])
	us.ns.c.logger.Info("receive push: "+s+" from: ", remoteAddr)

	var pushData pushData
	err1 := json.Unmarshal([]byte(s), &pushData)
	if err1 != nil {
		us.ns.c.logger.Info("failed to process push data.err:%+v", err1)
		return
	}
	ack := make(map[string]string)

	if pushData.PushType == "dom" || pushData.PushType == "service" {
		us.ns.updateServiceMap(pushData.Data)

		ack["type"] = "push-ack"
		ack["lastRefTime"] = strconv.FormatInt(pushData.LastRefTime, 10)
		ack["data"] = ""

	} else if pushData.PushType == "dump" {
		ack["type"] = "dump-ack"
		ack["lastRefTime"] = strconv.FormatInt(pushData.LastRefTime, 10)
		ack["data"] = encode(us.ns.serviceInfoMap)
	} else {
		ack["type"] = "unknow-ack"
		ack["lastRefTime"] = strconv.FormatInt(pushData.LastRefTime, 10)
		ack["data"] = ""
	}

	bs, _ := json.Marshal(ack)
	c, err := conn.WriteToUDP(bs, remoteAddr)
	if err != nil {
		us.ns.c.logger.Error("WriteToUDP failed,return:%d,err:%+v", c, err)
	}
}

func (us *pushReceiver) tryDecompressData(data []byte) string {

	if !IsGzipFile(data) {
		return string(data)
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))

	if err != nil {
		us.ns.c.logger.Error("failed to decompress gzip data,err:%+v", err)
		return ""
	}

	defer reader.Close()
	bs, err := ioutil.ReadAll(reader)

	if err != nil {
		us.ns.c.logger.Error("failed to decompress gzip data,err:%+v", err)
		return ""
	}

	return string(bs)
}

func IsGzipFile(data []byte) bool {
	if len(data) < 2 {
		return false
	}

	return bytes.HasPrefix(data, GZIP_MAGIC)
}
