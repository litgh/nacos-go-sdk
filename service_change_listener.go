package nacos

import (
	"sync"
)

type EventListener interface {
	OnEvent(*ServiceInfo)
}

type serviceChangeListener struct {
	sync.Mutex
	ns          *namingClient
	ch          chan *ServiceInfo
	observerMap map[string][]EventListener
}

func newServiceChangeListener(ns *namingClient) *serviceChangeListener {
	l := &serviceChangeListener{
		ns:          ns,
		ch:          make(chan *ServiceInfo, 0),
		observerMap: make(map[string][]EventListener),
	}
	go l.observe()
	return l
}

func (l *serviceChangeListener) addListener(serviceInfo *ServiceInfo, clusters string, listener EventListener) {
	l.Lock()
	defer l.Unlock()
	key := getServiceInfoKey(serviceInfo.Name, clusters)
	if v, ok := l.observerMap[key]; ok {
		v = append(v, listener)
	} else {
		l.observerMap[key] = []EventListener{listener}
	}
}

func (l *serviceChangeListener) removeListener(serviceName, clusters string, listener EventListener) {
	l.Lock()
	defer l.Unlock()
	key := getServiceInfoKey(serviceName, clusters)
	if v, ok := l.observerMap[key]; ok {
		for i, ol := range v {
			if ol == listener {
				v = append(v[:i], v[i+1:]...)
			}
		}
		if len(v) == 0 {
			delete(l.observerMap, key)
		}
	}
}

func (l *serviceChangeListener) isSubscribed(serviceName, clusters string) bool {
	l.Lock()
	defer l.Unlock()
	_, ok := l.observerMap[getServiceInfoKey(serviceName, clusters)]
	return ok
}

func (l *serviceChangeListener) serviceChange(serviceInfo *ServiceInfo) {
	if serviceInfo == nil {
		return
	}
	l.ch <- serviceInfo
}

func (l *serviceChangeListener) observe() {
	for {
		select {
		case <-l.ns.c.ctx.Done():
			return
		case s := <-l.ch:
			if v, ok := l.observerMap[s.GetKey()]; ok {
				for _, listener := range v {
					func(s *ServiceInfo) {
						defer func() {
							if err := recover(); err != nil {
								l.ns.c.Logger().Error("[RECOVER] notify err for service: %s, clusters: %v, %v", s.Name, s.Clusters, err)
							}
						}()
						listener.OnEvent(s)
					}(s)
				}
			}
		}
	}
}
