package nacos

import (
	"context"
	"sync"
)

type EventListener interface {
	OnEvent(*ServiceInfo)
}

type serviceChangeListener struct {
	sync.Mutex
	ctx         context.Context
	stop        context.CancelFunc
	ch          chan *ServiceInfo
	observerMap map[string][]EventListener
}

func newServiceChangeListener() *serviceChangeListener {
	ctx, cancel := context.WithCancel(context.Background())
	return &serviceChangeListener{
		ctx:         ctx,
		stop:        cancel,
		ch:          make(chan *ServiceInfo, 0),
		observerMap: make(map[string][]EventListener),
	}
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

func (l *serviceChangeListener) shutdown() {
	l.stop()
}

func (l *serviceChangeListener) observe() {
	for {
		select {
		case <-l.ctx.Done():
			return
		case s := <-l.ch:
			if v, ok := l.observerMap[s.GetKey()]; ok {
				for _, listener := range v {
					listener.OnEvent(s)
				}
			}
		}
	}
}
