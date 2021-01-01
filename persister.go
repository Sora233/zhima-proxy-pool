package zhima_proxy_pool

import (
	"encoding/json"
	"io/ioutil"
)

/*
Persister define a persister interface.
It saves and loads the active proxy, or you will load new proxy every time you restart, which is not Economic.
*/
type Persister interface {
	Save([]*Proxy) error
	Load() ([]*Proxy, error)
}

// FilePersister is a file based persister
type FilePersister struct {
	FilePath string
}

// Save the proxy to file with json format
func (f *FilePersister) Save(proxies []*Proxy) error {
	if proxies == nil {
		return nil
	}
	bproxy, err := json.Marshal(proxies)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(f.FilePath, bproxy, 0644)
}

// Load []*Proxy from the file
func (f *FilePersister) Load() ([]*Proxy, error) {
	bproxy, err := ioutil.ReadFile(f.FilePath)
	if err != nil {
		return nil, err
	}

	var proxies = make([]*Proxy, 0)

	err = json.Unmarshal(bproxy, &proxies)
	if err != nil {
		return nil, err
	}
	return proxies, nil
}

// NewFilePersister return a FilePersister instance
func NewFilePersister(path string) *FilePersister {
	return &FilePersister{
		FilePath: path,
	}
}

// NilPersister is a nil persister, save nothing and load nothing
type NilPersister struct{}

// Save nothing
func (e *NilPersister) Save(proxies []*Proxy) error {
	return nil
}

// Load nothing
func (e *NilPersister) Load() ([]*Proxy, error) {
	return nil, nil
}

// NewNilPersister return a NilPersister instance
func NewNilPersister() *NilPersister {
	return &NilPersister{}
}
