/*
  An Economic ProxyPool Implement for ZhimaHTTP (http://h.zhimaruanjian.com/)
*/
package zhima_proxy_pool

import (
	"container/list"
	"errors"
	"fmt"
	"github.com/asmcos/requests"
	"github.com/sirupsen/logrus"
	"sync"
	"time"
)

var logger = logrus.StandardLogger()

// Config is the config for ZhimaProxyPool
type Config struct {
	// ApiAddr is the zhima api http address
	ApiAddr string
	// BackUpCap is the backup proxy size limit, all backup proxy is costless until they become active one.
	BackUpCap int
	// ActiveCap is the active proxy size limit, all active proxy is paid already.
	ActiveCap int
	// ClearTime is the duration before the backup proxy cleared.
	ClearTime time.Duration
	// TimLimit is the proxy expire time, depend on your plan.
	TimeLimit time.Duration
}

type Proxy struct {
	Ip               string `json:"ip"`
	Port             int    `json:"port"`
	ExpireTimeString string `json:"expire_time"`
}

// ExpireTime() return a time.Time parsed from ExpireTimeString
func (p *Proxy) ExpireTime() (time.Time, error) {
	return time.ParseInLocation("2006-01-02 15:04:05", p.ExpireTimeString, time.Local)
}

func (p *Proxy) ProxyString() string {
	return fmt.Sprintf("%v:%v", p.Ip, p.Port)
}

// Expired() return whether the proxy has expired
func (p *Proxy) Expired() bool {
	t, err := p.ExpireTime()
	if err != nil {
		return true
	}
	return t.Before(time.Now().Add(time.Second * 10))
}

type response struct {
	Code    int      `json:"code"`
	Data    []*Proxy `json:"data"`
	Msg     string   `json:"msg"`
	Success bool     `json:"success"`
}

// ZhimaProxyPool is the proxy pool implement
type ZhimaProxyPool struct {
	Config      *Config
	api         string
	backupProxy *list.List
	activeProxy []*Proxy
	*sync.Cond
	activeMutex *sync.RWMutex
	persister   Persister
	index       int
}

// Start() start the background task
func (pool *ZhimaProxyPool) Start() {
	go pool.fillBackup()
	go func() {
		ticker := time.NewTicker(pool.Config.ClearTime)
		for {
			select {
			case <-ticker.C:
				go pool.Clear()
			}
		}
	}()
	pool.activeMutex.Lock()
	defer pool.activeMutex.Unlock()
	for len(pool.activeProxy) < pool.Config.ActiveCap {
		backup, err := pool.popBackup()
		if err != nil {
			logger.Errorf("fill active proxy failed %v", err)
		} else {
			pool.activeProxy = append(pool.activeProxy, backup)
		}
	}
}

// Clear() clear the backup proxy list
func (pool *ZhimaProxyPool) Clear() {
	pool.L.Lock()
	defer pool.L.Unlock()
	pool.backupProxy = list.New()
}

func (pool *ZhimaProxyPool) fillBackup() {
	for {
		pool.L.Lock()

		for pool.checkBackup() {
			pool.Broadcast()
			pool.Wait()
		}
		logger.WithField("backup size", pool.backupProxy.Len()).Debug("backup proxy not enough... fresh")

		var loopCount = 0

		for pool.backupProxy.Len() < pool.Config.BackUpCap {
			if loopCount >= 5 {
				logger.WithField("backup size", pool.backupProxy.Len()).
					Errorf("can not get enough backup proxy after fetch 5 times, check your timeLimit or backupCap")
				break
			}
			loopCount += 1
			resp, err := requests.Get(pool.api)
			if err != nil {
				logger.Errorf("fresh failed %v", err)
				pool.L.Unlock()
				break
			}
			zhimaResp := new(response)
			err = resp.Json(zhimaResp)
			if err != nil {
				logger.Errorf("parse zhima response failed %v", err)
				pool.L.Unlock()
				break
			}
			if zhimaResp.Code != 0 {
				log := logger.WithField("code", zhimaResp.Code).
					WithField("msg", zhimaResp.Msg)
				switch zhimaResp.Code {
				case 111:
					time.Sleep(time.Second * 5)
				default:
					log.Errorf("fresh failed")
				}
			} else {
				now := time.Now()
				for _, proxy := range zhimaResp.Data {
					t, err := proxy.ExpireTime()
					if err != nil {
						continue
					}
					if t.Sub(now) >= pool.Config.TimeLimit {
						pool.backupProxy.PushBack(proxy)
					}
				}
			}
		}
		if pool.checkBackup() {
			pool.Broadcast()
		}
		logger.WithField("backup size", pool.backupProxy.Len()).Debug("backup freshed")
		pool.L.Unlock()
	}
}

// Get() try to get a usable Proxy.
// First, get a active proxy, if it has expired, replace it with a backup proxy and return the new proxy.
func (pool *ZhimaProxyPool) Get() (*Proxy, error) {
	var result *Proxy
	pool.activeMutex.RLock()

	if len(pool.activeProxy) == 0 {
		return nil, errors.New("active proxy empty, please check your config or report bug")
	}

	pos := pool.index
	pool.index = (pool.index + 1) % pool.Config.ActiveCap

	result = pool.activeProxy[pos]
	if result.Expired() {
		pool.activeMutex.RUnlock()
		pool.activeMutex.Lock()
		result = pool.activeProxy[pos]
		if result.Expired() {
			err := pool.replaceActive(pos)
			if err != nil {
				pool.activeMutex.Unlock()
				return nil, err
			}
		}
		pool.activeMutex.Unlock()
		pool.activeMutex.RLock()
	}
	result = pool.activeProxy[pos]
	pool.activeMutex.RUnlock()

	//logger.WithField("return proxy", result).WithField("all", pool.activeProxy).Debug("proxy")
	return result, nil
}

// Delete() remove a Proxy from active proxy list.
// Use it when you make sure the proxy is not usable.
// Abuse this may cost more.
func (pool *ZhimaProxyPool) Delete(p *Proxy) bool {
	pool.L.Lock()
	defer pool.L.Unlock()

	var result = false

	for index, curProxy := range pool.activeProxy {
		if curProxy.ProxyString() == p.ProxyString() {
			err := pool.replaceActive(index)
			if err == nil {
				result = true
			}
		}
	}
	return result
}

// Stop() call the persister.Save, this will not stop pool actually.
func (pool *ZhimaProxyPool) Stop() error {
	pool.L.Lock()
	defer pool.L.Unlock()
	return pool.persister.Save(pool.activeProxy)
}

func (pool *ZhimaProxyPool) replaceActive(index int) (err error) {
	log := logger.WithField("deleted_proxy", pool.activeProxy[index].ProxyString()).WithField("old_expire", pool.activeProxy[index].ExpireTime)
	oldProxy := pool.activeProxy[index]
	newProxy, err := pool.popBackup()
	if err != nil {
		return err
	}
	if oldProxy == pool.activeProxy[index] {
		pool.activeProxy[index] = newProxy
		log.WithField("new_proxy", pool.activeProxy[index].ProxyString()).WithField("new_expire", pool.activeProxy[index].ExpireTime).Debug("deleted")
	}
	return nil
}

func (pool *ZhimaProxyPool) loadActive(loader func() ([]*Proxy, error)) error {
	pool.L.Lock()
	defer pool.L.Unlock()

	loaded, err := loader()
	if err != nil {
		return err
	}
	for _, proxy := range loaded {
		if !proxy.Expired() {
			pool.activeProxy = append(pool.activeProxy, proxy)
			if len(pool.activeProxy) == pool.Config.ActiveCap {
				break
			}
		}
	}
	return nil
}

// caller must hold the lock
func (pool *ZhimaProxyPool) popBackup() (*Proxy, error) {
	for !pool.checkBackup() {
		pool.Signal()
		pool.Wait()
	}
	backup := pool.backupProxy.Front()
	pool.backupProxy.Remove(backup)
	return backup.Value.(*Proxy), nil
}

func (pool *ZhimaProxyPool) checkBackup() bool {
	return pool.backupProxy.Len() != 0
}

// NewZhimaProxyPool() return a ZhimaProxyPool instance
func NewZhimaProxyPool(config *Config, persister Persister) *ZhimaProxyPool {
	activeMutex := new(sync.RWMutex)
	pool := &ZhimaProxyPool{
		Config:      config,
		activeProxy: make([]*Proxy, 0),
		backupProxy: list.New(),
		Cond:        sync.NewCond(activeMutex),
		activeMutex: activeMutex,
		persister:   persister,
	}
	if err := pool.loadActive(pool.persister.Load); err != nil {
		logger.WithField("active size", len(pool.activeProxy)).Debug("load err %v", err)
	} else {
		logger.WithField("active size", len(pool.activeProxy)).Debug("load ok")
	}
	pool.Start()
	return pool
}
