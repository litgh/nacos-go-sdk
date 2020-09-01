package nacos

import (
	"net/url"
	"os"
	"strings"
	"testing"

	. "github.com/echocat/gocheck-addons"
	. "gopkg.in/check.v1"
)

type MySuite struct {
	ns NamingClient
}

var _ = Suite(&MySuite{})

var (
	hosts       = os.Getenv("NACOS_TEST_HOSTS")
	namespace   = os.Getenv("NACOS_TEST_NAMESPACE")
	username    = os.Getenv("NACOS_TEST_USERNAME")
	password    = os.Getenv("NACOS_TEST_PASSWORD")
	serviceName = "go-sdk-test"
	groupName   = DefaultGroup
	clusterName = "testGroup"
	ip          = GetLocalIP()
	port        = 8800
)

func Test(t *testing.T) {
	TestingT(t)
}

func (m *MySuite) SetUpSuite(c *C) {
	if hosts == "" {
		c.Fatal("NACOS_TEST_HOSTS not specified")
	}
	config := new(Config)
	hostArr := strings.Split(hosts, ",")
	for _, h := range hostArr {
		_url, err := url.Parse(h)
		if err != nil {
			c.Fatal(err)
		}
		config.Scheme = _url.Scheme
		config.ContextPath = _url.Path
		config.Hosts = append(config.Hosts, _url.Host)
	}
	config.Namespace = namespace
	config.Username = username
	config.Password = password
	config.LogLevel = LogDebug
	client, err := NewClient(config)
	if err != nil {
		c.Fatal(err)
	}
	client.Logger().Debug("config.Scheme: %s", config.Scheme)
	client.Logger().Debug("config.ContextPath: %s", config.ContextPath)
	client.Logger().Debug("config.Hosts: %s", config.Hosts)
	m.ns = client.Naming()

	resp, err := m.ns.CreateService(ServiceOptions{
		ServiceName: serviceName,
		GroupName:   groupName,
		Metadata:    NewMetadata(nil).Put("foo", "bar"),
	})
	c.Assert(err, IsNil)
	c.Assert(resp.Ok(), Equals, true)
}

func (m *MySuite) TearDownSuite(c *C) {
	m.ns.DeRegisterInstance(serviceName, groupName, clusterName, ip, port, false)
	resp, err := m.ns.DeleteService(ServiceOptions{
		ServiceName: serviceName,
		GroupName:   groupName,
	})
	c.Assert(err, IsNil)
	c.Assert(resp.Ok(), Equals, true)
}

func (m *MySuite) TestSelectService(c *C) {
	list, err := m.ns.SelectServices(ServiceQueryOptions{})
	c.Assert(err, IsNil)
	c.Assert(list.Count, IsLargerThan, 0)

	serviceInfo, err := m.ns.SelectService(ServiceQueryOptions{
		ServiceName: serviceName,
	})
	c.Assert(err, IsNil)
	c.Assert(serviceInfo.Name, Equals, serviceName)
	c.Assert(NewMetadata(serviceInfo.Metadata).Contains("foo"), Equals, true)
	c.Assert(serviceInfo.GroupName, Equals, groupName)
}

func (m *MySuite) TestRegisterInstance(c *C) {
	resp, err := m.ns.RegisterInstance(NewInstance(serviceName, groupName, clusterName, ip, port, 1.0, true, false, NewMetadata(nil).Put("foo", "bar")))
	c.Assert(err, IsNil)
	c.Assert(resp.Ok(), Equals, true)

	instances := m.ns.SelectInstance(InstanceQueryOptions{
		ServiceName: serviceName,
		GroupName:   groupName,
		ClusterName: []string{clusterName},
	})
	c.Assert(len(instances), Equals, 1)
	c.Assert(instances[0].ServiceName, Equals, serviceName)
	c.Assert(instances[0].Healthy, Equals, true)
	c.Assert(instances[0].IP, Equals, ip)
	c.Assert(instances[0].Port, Equals, port)
	c.Assert(instances[0].ClusterName, Equals, clusterName)

	resp, err = m.ns.DeRegisterInstance(serviceName, groupName, clusterName, ip, port, true)
	c.Assert(err, IsNil)
	c.Assert(resp.Ok(), Equals, true)

}
