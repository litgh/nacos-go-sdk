package nacos

import (
	"bytes"
	"sync"
	"time"
)

const (
	metaKeyHeartBeatInterval        = "preserved.heart.beat.interval"
	metaKeyHeartBeatIntervalDefault = time.Second * 5
)

type Metadata struct {
	sync.Mutex
	m map[string]string
}

func NewMetadata(m map[string]string) *Metadata {
	if m == nil {
		m = make(map[string]string)
	}
	return &Metadata{
		m: m,
	}
}

func (m *Metadata) Put(key, value string) *Metadata {
	m.Lock()
	defer m.Unlock()
	m.m[key] = value
	return m
}

func (m *Metadata) Encode() string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.Encode(m.m)
	return buf.String()
}

func (m *Metadata) Get(key string) string {
	m.Lock()
	defer m.Unlock()
	return m.m[key]
}

func (m *Metadata) GetOrDefault(key string, defaultValue string) string {
	m.Lock()
	defer m.Unlock()
	v, ok := m.m[key]
	if ok {
		return v
	}
	return defaultValue
}

func (m *Metadata) GetWithDefault(key string, defaultValue interface{}, convertFn func(value string) (interface{}, error)) interface{} {
	m.Lock()
	defer m.Unlock()
	v, ok := m.m[key]
	if ok {
		vv, err := convertFn(v)
		if err != nil {
			return defaultValue
		}
		return vv
	}
	return defaultValue
}

func (m Metadata) Contains(key string) bool {
	m.Lock()
	defer m.Unlock()
	_, ok := m.m[key]
	return ok
}
