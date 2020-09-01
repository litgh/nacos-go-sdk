package nacos

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var beatSignal = struct{}{}

type heartbeat struct {
	nc               *namingClient
	dom2Beat         map[string]*beat
	lightBeatEnabled bool
}

func newHeartbeat(nc *namingClient) *heartbeat {
	return &heartbeat{
		nc:       nc,
		dom2Beat: make(map[string]*beat),
	}
}

type beat struct {
	serviceName string
	cluster     string
	ip          string
	port        int
	weight      float64
	metadata    *Metadata
	scheduled   bool
	stop        context.CancelFunc
	next        chan struct{}
	period      int64
}

func newBeat(groupedServiceName string, instance *Instance) *beat {
	return &beat{
		serviceName: groupedServiceName,
		cluster:     instance.ClusterName,
		ip:          instance.IP,
		port:        instance.Port,
		weight:      instance.Weight,
		metadata:    instance.Metadata,
		scheduled:   false,
		next:        make(chan struct{}),
		period: instance.Metadata.GetWithDefault(metaKeyHeartBeatInterval, metaKeyHeartBeatIntervalDefault.Milliseconds(), func(value string) (interface{}, error) {
			d, err := time.ParseDuration(value)
			if err != nil {
				return nil, err
			}
			return d.Milliseconds(), nil
		}).(int64),
	}
}

func (h *heartbeat) addBeat(beatInfo *beat) {
	key := buildKey(beatInfo.serviceName, beatInfo.ip, beatInfo.port)

	if b, ok := h.dom2Beat[key]; ok {
		b.stop()
		delete(h.dom2Beat, key)
	}

	h.dom2Beat[key] = beatInfo
	ctx, cancel := context.WithCancel(context.Background())
	beatInfo.stop = cancel

	go func(ctx context.Context, beatInfo *beat) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-beatInfo.next:
				h.sendBeat(beatInfo)
			}
		}
	}(ctx, beatInfo)
	go h.schedule(beatInfo, time.Millisecond*time.Duration(beatInfo.period))
}

func (h *heartbeat) removeBeat(serviceName, ip string, port int) {
	key := buildKey(serviceName, ip, port)
	if beatInfo, ok := h.dom2Beat[key]; ok {
		beatInfo.stop()
	}
}

func (h *heartbeat) sendBeat(beat *beat) {
	r := h.nc.NewRequest(PUT, "/instance/beat")
	r.params.Set("namespaceId", h.nc.c.config.Namespace)
	r.params.Set("serviceName", beat.serviceName)
	r.params.Set("clusterName", beat.cluster)
	r.params.Set("ip", beat.ip)
	r.params.Set("port", strconv.Itoa(beat.port))
	if !h.lightBeatEnabled {
		r.body = strings.NewReader(encode(map[string]string{
			"beat": encode(beat),
		}))
	}
	result, _ := callServer(h.nc.c, r)
	if result.Ok() {
		var m = make(map[string]interface{})
		json.Unmarshal([]byte(result.Data), &m)
		if v, ok := m["lightBeatEnabled"]; ok {
			h.lightBeatEnabled = v.(bool)
		}
		if v, ok := m["clientBeatInterval"]; ok {
			go h.schedule(beat, time.Millisecond*time.Duration(v.(int64)))
		}
	}
	// else if result.Code == 20404 {
	// 	// register instance
	// 	beat.stop()
	// 	ins := NewInstance(beat.ip, beat.port, beat.weight, beat.cluster, beat.metadata)
	// 	ins.ServiceName = beat.serviceName
	// 	ins.Ephemeral = true
	// 	h.nc.RegisterInstance(ins)
	// } else {
	// 	// TODO log error

	// }
}

func (h *heartbeat) schedule(beatInfo *beat, d time.Duration) {
	t := time.NewTimer(d)
	for {
		select {
		case <-t.C:
			beatInfo.next <- beatSignal
			t.Stop()
			return
		}
	}
}

func (h *heartbeat) updateBeatInfo(hosts []*Instance) {
	for _, host := range hosts {
		key := buildKey(host.ServiceName, host.IP, host.Port)
		if _, ok := h.dom2Beat[key]; ok && host.Ephemeral {
			beat := newBeat(host.ServiceName, host)
			h.addBeat(beat)
		}
	}
}

func buildKey(serviceName, ip string, port int) string {
	return fmt.Sprintf("%s#%s#%d", serviceName, ip, port)
}
