package nacos

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var _ NamingClient = new(namingClient)

type SelectorType string

const (
	serviceInfoSpliter   = "@@"
	vipSrvRefInterMillis = 30
	SelectorTypeNone     = "none"
	SelectorTypeUnknown  = "unknown"
	SelectorTypeLabel    = "label"
)

type namingClient struct {
	c                     *client
	heartbeat             *heartbeat
	failover              *failover
	pushReceiver          *pushReceiver
	listeners             *serviceChangeListener
	serviceInfoMap        map[string]*ServiceInfo
	lastServerRefreshTime int64
	serversFromEndpoint   []string
}

func (ns *namingClient) refreshSrvIfNeed() error {
	if len(ns.c.config.Hosts) != 0 {
		// server list provide by user
		return nil
	}
	if time.Now().Unix()-ns.lastServerRefreshTime < vipSrvRefInterMillis {
		return nil
	}
	// get server list from endpoint
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://%s/nacos/serverlist", ns.c.config.Endpoint), nil)
	ns.c.setHeader(req.Header)

	resp, err := ns.c.config.HttpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Error while requesting: %s. Server returned: %s", ns.c.config.Endpoint, err)
	}
	var response Response
	ns.c.decode(resp, &response)
	if !response.Ok() {
		return fmt.Errorf("Error while requesting: %s. Server returned: %s", ns.c.config.Endpoint, response.Message)
	}
	br := bufio.NewReader(strings.NewReader(response.Data))
	var serverList []string
	for {
		l, err := br.ReadSlice('\n')
		if err == io.EOF {
			break
		}
		serverList = append(serverList, strings.TrimSpace(string(l)))
	}
	if len(serverList) == 0 {
		return fmt.Errorf("Cannot acquire Nacos server")
	}
	ns.serversFromEndpoint = serverList
	ns.lastServerRefreshTime = time.Now().Unix()

	return nil
}

func (ns *namingClient) getServerList() []string {
	if len(ns.c.config.Hosts) > 0 {
		return ns.c.config.Hosts
	}
	return ns.serversFromEndpoint
}

func (ns *namingClient) NewRequest(method, path string) *Request {
	r := &Request{
		config: &ns.c.config,
		method: method,
		path:   "/v1/ns" + path,
		params: make(url.Values),
		header: make(http.Header),
	}
	return r
}

type Selector struct {
	Type SelectorType `json:"type"`
}

type ServiceList struct {
	Service []string `json:"doms"`
	Count   int      `json:"count"`
}

type Service struct {
	Name             string
	GroupName        string
	AppName          string
	Metadata         map[string]string
	ProtectThreshold float64
}

type ServiceInfo struct {
	Name           string
	GroupName      string
	Clusters       string
	JsonFromServer string
	CacheMillis    int64
	Hosts          []*Instance
	LastRefTime    int64
	Checksum       string
	AllIPs         bool
}

func NewServiceInfo(name, groupName, clusters string) *ServiceInfo {
	return &ServiceInfo{Name: name, GroupName: groupName, Clusters: clusters}
}

func NewServiceInfoByKey(key string) *ServiceInfo {
	keys := strings.Split(key, serviceInfoSpliter)
	var serviceInfo = new(ServiceInfo)
	if len(keys) >= 2 {
		serviceInfo.Name = keys[0]
		serviceInfo.Clusters = keys[1]
	}
	serviceInfo.Name = keys[0]
	return serviceInfo
}

func (s *ServiceInfo) GetKey() string {
	if s.Clusters != "" {
		return getServiceInfoKey(s.Name, s.Clusters)
	}
	return s.Name
}

func (s *ServiceInfo) Validate() bool {
	return true
}

func getServiceInfoKey(groupServiceName, clusters string) string {
	if clusters != "" {
		return groupServiceName + serviceInfoSpliter + clusters
	}

	return groupServiceName
}

// ServiceOptions options
type ServiceOptions struct {
	// name of service
	ServiceName string
	// group of service
	GroupName string
	// expression of selector
	Expression string
	// protectThreshold of service
	ProtectThreshold float64
	// metadata of service
	Metadata *Metadata
	Selector *Selector
}

// ServiceQueryOptions query options
type ServiceQueryOptions struct {
	Page        int
	Size        int
	GroupName   string
	ServiceName string
	Selector    Selector
}

func setServiceQueryOptions(r *Request, q ServiceQueryOptions) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.Size <= 0 {
		q.Size = 10
	}
	if q.GroupName == "" {
		q.GroupName = DefaultGroup
	}

	r.params.Set("pageNo", strconv.Itoa(q.Page))
	r.params.Set("pageSize", strconv.Itoa(q.Size))
	r.params.Set("groupName", q.GroupName)

	if q.ServiceName != "" {
		r.params.Set("serviceName", q.ServiceName)
	}

}

func setServiceOptions(r *Request, q *ServiceOptions) error {
	form := make(url.Values)
	if q.ServiceName == "" {
		return errors.New("ERR: serviceName is required")
	}
	if q.GroupName == "" {
		q.GroupName = DefaultGroup
	}
	form.Set("serviceName", q.ServiceName)
	form.Set("groupName", q.GroupName)
	form.Set("protectThreshold", strconv.FormatFloat(q.ProtectThreshold, 'f', 2, 64))
	if q.Metadata != nil {
		form.Set("metadata", q.Metadata.Encode())
	}
	if q.Selector == nil {
		form.Set("selector", encode(&Selector{
			Type: SelectorTypeNone,
		}))
	} else {
		form.Set("selector", encode(q.Selector))
	}

	r.body = strings.NewReader(form.Encode())
	return nil
}

func (ns *namingClient) SelectServices(q ServiceQueryOptions) (*ServiceList, error) {
	r := ns.NewRequest(GET, "/service/list")
	setServiceQueryOptions(r, q)
	resp, err := ns.c.DoRequest(r)
	if err != nil {
		return nil, err
	}
	var serviceList ServiceList
	err = decode(resp, &serviceList)
	if ns.c.logger.IsDebugEnable() {
		ns.c.logger.Debug("%s", encode(serviceList))
	}
	return &serviceList, err
}

func (ns *namingClient) SelectService(q ServiceQueryOptions) (*Service, error) {
	r := ns.NewRequest(GET, "/service")
	setServiceQueryOptions(r, q)
	resp, err := ns.c.DoRequest(r)
	if err != nil {
		return nil, err
	}
	var service Service
	err = decode(resp, &service)
	if err == nil && ns.c.logger.IsDebugEnable() {
		ns.c.logger.Debug("%s", encode(service))
	}
	return &service, err
}

func (ns *namingClient) CreateService(q ServiceOptions) (*Response, error) {
	r := ns.NewRequest(POST, "/service")
	setServiceOptions(r, &q)
	return callServer(ns.c, r)
}

func (ns *namingClient) DeleteService(q ServiceOptions) (*Response, error) {
	r := ns.NewRequest(DELETE, "/service")
	setServiceOptions(r, &q)
	return callServer(ns.c, r)
}

func (ns *namingClient) UpdateService(q ServiceOptions) (*Response, error) {
	r := ns.NewRequest(PUT, "/service")
	setServiceOptions(r, &q)
	return callServer(ns.c, r)
}

type InstanceQueryOptions struct {
	ServiceName string
	GroupName   string
	ClusterName []string
	Subscribe   bool
	Healthy     bool
}

type Instance struct {
	GroupName   string
	ServiceName string
	ClusterName string
	InstanceID  string
	IP          string
	Port        int
	Weight      float64
	Healthy     bool
	Enable      bool
	Ephemeral   bool
	Metadata    *Metadata
}

func NewInstance(serviceName, groupName, clusterName string, ip string, port int, weight float64, enable, ephemeral bool, metadata *Metadata) *Instance {
	return &Instance{
		ServiceName: serviceName,
		GroupName:   groupName,
		ClusterName: clusterName,
		InstanceID:  uuid.New().String(),
		IP:          ip,
		Port:        port,
		Weight:      weight,
		Metadata:    metadata,
		Enable:      enable,
		Ephemeral:   ephemeral,
	}
}

func (i *Instance) toInetAddr() string {
	return fmt.Sprintf("%s:%d", i.IP, i.Port)
}

func (i *Instance) equals(o *Instance) bool {
	return (i.GroupName == o.GroupName && i.ServiceName == o.ServiceName &&
		i.ClusterName == o.ClusterName && i.InstanceID == o.InstanceID && i.IP == o.IP &&
		i.Port == o.Port && i.Weight == o.Weight && i.Healthy == o.Healthy && i.Enable == o.Enable &&
		i.Ephemeral == o.Ephemeral && i.Metadata.Encode() == o.Metadata.Encode())
}

func setInstanceOptions(r *Request, instance *Instance) {
	if instance.GroupName != "" {
		instance.GroupName = DefaultGroup
	}
	if instance.ClusterName == "" {
		instance.ClusterName = DefaultCluster
	}
	r.params.Set("serviceName", fmt.Sprintf("%s%s%s", instance.GroupName, serviceInfoSpliter, instance.ServiceName))
	r.params.Set("groupName", instance.GroupName)
	r.params.Set("clusterName", instance.ClusterName)
	r.params.Set("ip", instance.IP)
	r.params.Set("port", strconv.Itoa(instance.Port))
	r.params.Set("weight", strconv.FormatFloat(instance.Weight, 'f', 2, 64))
	r.params.Set("enable", strconv.FormatBool(instance.Enable))
	r.params.Set("healthy", strconv.FormatBool(instance.Healthy))
	r.params.Set("ephemeral", strconv.FormatBool(instance.Ephemeral))
	r.params.Set("metadata", instance.Metadata.Encode())
}

func (ns *namingClient) RegisterInstance(instance *Instance) (*Response, error) {
	r := ns.NewRequest(POST, "/instance")
	setInstanceOptions(r, instance)
	rs, err := callServer(ns.c, r)
	if err != nil {
		return nil, err
	}
	if instance.Ephemeral {
		beatInfo := newBeat(r.params.Get("serviceName"), instance)
		ns.heartbeat.addBeat(beatInfo)
	}
	return rs, nil
}

func (ns *namingClient) DeRegisterInstance(serviceName, groupName, clusterName, ip string, port int, ephemeral bool) (*Response, error) {
	if ephemeral {
		ns.heartbeat.removeBeat(fmt.Sprintf("%s%s%s", groupName, serviceInfoSpliter, serviceName), ip, port)
	}
	r := ns.NewRequest(DELETE, "/instance")
	r.params.Set("serviceName", serviceName)
	r.params.Set("clusterName", clusterName)
	r.params.Set("ip", ip)
	r.params.Set("port", strconv.Itoa(port))
	r.params.Set("ephemeral", strconv.FormatBool(ephemeral))
	return callServer(ns.c, r)
}

func (ns *namingClient) SelectInstance(q InstanceQueryOptions) []*Instance {
	if q.ClusterName == nil || len(q.ClusterName) == 0 {
		q.ClusterName = []string{DefaultCluster}
	}
	var serviceInfo *ServiceInfo
	if q.Subscribe {
		serviceInfo = ns.getServiceInfo(q.ServiceName, q.GroupName, strings.Join(q.ClusterName, ","))
	} else {
		serviceInfo = ns.getServiceInfoDirectlyFromServer(q.ServiceName, q.GroupName, strings.Join(q.ClusterName, ","))
	}
	if serviceInfo == nil {
		return nil
	}
	return serviceInfo.Hosts

}
func (ns *namingClient) Subscribe(serviceName, groupName string, clusters []string, listener EventListener) {
	serviceInfo := ns.getServiceInfo(serviceName, groupName, strings.Join(clusters, ","))
	ns.listeners.addListener(serviceInfo, strings.Join(clusters, ","), listener)
}
func (ns *namingClient) Unsubscribe(serviceName, groupName string, clusters []string, listener EventListener) {
	ns.listeners.removeListener(groupName+serviceInfoSpliter+serviceName, strings.Join(clusters, ","), listener)
}
func (ns *namingClient) Shutdown() {
	ns.c.cancel()
}

func (ns *namingClient) getServiceInfo(serviceName, groupName, clusters string) *ServiceInfo {
	key := getServiceInfoKey(groupName+serviceInfoSpliter+serviceName, clusters)
	if ns.failover.isFailoverSwitch() {
		return ns.failover.getService(key)
	}
	serviceInfo, ok := ns.serviceInfoMap[key]
	if !ok {
		serviceInfo = NewServiceInfo(serviceName, groupName, clusters)
		ns.updateServiceInfoNow(serviceInfo)
	}
	return serviceInfo
}

func (ns *namingClient) getServiceInfoDirectlyFromServer(serviceName, groupName, clusters string) *ServiceInfo {
	rs, err := ns.queryList(groupName+serviceInfoSpliter+serviceName, clusters, 0, false)
	if err != nil {
		return nil
	}
	var serviceInfo ServiceInfo
	err = json.Unmarshal([]byte(rs.Data), &serviceInfo)
	if err != nil {
		return nil
	}
	return &serviceInfo
}

func (ns *namingClient) queryList(groupedServiceName, clusters string, udpPort int, healthyOnly bool) (*Response, error) {
	r := ns.NewRequest(GET, "/instance/list")
	r.params.Set("serviceName", groupedServiceName)
	r.params.Set("clusters", clusters)
	r.params.Set("udpPort", strconv.Itoa(udpPort))
	r.params.Set("clientIP", GetLocalIP())
	r.params.Set("healthyOnly", strconv.FormatBool(healthyOnly))
	return callServer(ns.c, r)
}

func (ns *namingClient) updateServiceInfoNow(serviceInfo *ServiceInfo) {
	rs, _ := ns.queryList(serviceInfo.GetKey(), serviceInfo.Clusters, ns.pushReceiver.port, false)
	if rs.Ok() {
		ns.updateServiceMap(rs.Data)
	}
}

func (ns *namingClient) updateServiceMap(serviceJSON string) *ServiceInfo {
	var serviceInfo ServiceInfo
	err := json.Unmarshal([]byte(serviceJSON), &serviceInfo)
	if err != nil {
		return nil
	}
	key := serviceInfo.GetKey()
	oldServiceInfo, ok := ns.serviceInfoMap[key]
	if serviceInfo.Hosts == nil || !serviceInfo.Validate() {
		return oldServiceInfo
	}
	var changed bool
	if ok {
		if oldServiceInfo.LastRefTime > serviceInfo.LastRefTime {
			//
		}
		ns.serviceInfoMap[key] = &serviceInfo
		oldHostMap := make(map[string]*Instance)
		newHostMap := make(map[string]*Instance)

		for _, h := range oldServiceInfo.Hosts {
			oldHostMap[h.toInetAddr()] = h
		}
		for _, h := range serviceInfo.Hosts {
			newHostMap[h.toInetAddr()] = h
		}

		modHosts := make([]*Instance, 0)
		newHosts := make([]*Instance, 0)
		removeHosts := make([]*Instance, 0)

		for k, v := range newHostMap {
			if oh, ok := oldHostMap[k]; ok && !v.equals(oh) {
				modHosts = append(modHosts, v)
			} else if !ok {
				newHosts = append(newHosts, v)
			}
		}

		for k, v := range oldHostMap {
			if _, ok := newHostMap[k]; ok {
				continue
			} else if !ok {
				removeHosts = append(removeHosts, v)
			}
		}

		if len(newHosts) > 0 {
			//
			changed = true
		}
		if len(removeHosts) > 0 {
			//
			changed = true
		}
		if len(modHosts) > 0 {
			changed = true
			ns.heartbeat.updateBeatInfo(modHosts)
		}
		serviceInfo.JsonFromServer = serviceJSON
		if len(newHosts) > 0 || len(removeHosts) > 0 || len(modHosts) > 0 {
			// TODO event dispatch
			writeCache(ns.c.config.CacheDir, &serviceInfo)
		}
	} else {
		changed = true
		ns.serviceInfoMap[key] = &serviceInfo
		serviceInfo.JsonFromServer = serviceJSON
		// TODO event dispatch
		writeCache(ns.c.config.CacheDir, &serviceInfo)
	}
	if changed {
		ns.listeners.serviceChange(&serviceInfo)
	}

	return &serviceInfo

}

func writeCache(cacheDir string, serviceInfo *ServiceInfo) {
	file := path.Join(cacheDir, getServiceInfoKey(url.QueryEscape(serviceInfo.Name), serviceInfo.Clusters))
	ioutil.WriteFile(file, []byte(serviceInfo.JsonFromServer), os.ModePerm)
}
