package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"github.com/antchfx/xmlquery"
	"github.com/dgraph-io/badger"
	"github.com/helloyi/go-sshclient"
	"gopkg.in/yaml.v2"
	"log"
	"os"
	"strings"
	"time"
)

func GetName(node *xmlquery.Node) string {
	n := xmlquery.FindOne(node, "/name")
	if n != nil {
		return n.InnerText()
	}
	return ""
}

func GetPath(node *xmlquery.Node) string {
	var ret []string
	for node != nil {
		name := GetName(node)
		if len(name) > 0 {
			ret = append(ret, name)
		}
		ret = append(ret, node.Data)
		node = node.Parent
	}
	for i, j := 0, len(ret)-1; i < j; i, j = i+1, j-1 {
		ret[i], ret[j] = ret[j], ret[i]
	}
	if len(ret) == 0 {
		return ""
	}
	if ret[0] == "" {
		ret = ret[1:]
	}
	if len(ret) > 1 && ret[0] == "rpc-reply" {
		ret = ret[1:]
	}
	if len(ret) > 1 && ret[0] == "configuration" {
		ret = ret[1:]
	}
	//remove interfaces outer to interface node
	stop := false
	for !stop {
		stop = true
		for i := range ret[1:] {
			if ret[i] == ret[i+1]+"s" {
				ret = remove(ret, i)
				stop = false
				break
			}
		}
	}
	return strings.Join(ret, " ")
}

func remove(slice []string, s int) []string {
	return append(slice[:s], slice[s+1:]...)
}

type SSHClient struct {
	User       string `yaml:"user"`
	Pass       string `yaml:"pass,omitempty"`
	KeyFile    string `yaml:"key-file,omitempty"`
	NumWorkers *int   `yaml:"num-workers"`
}

type Config struct {
	SSHClient SSHClient `yaml:"ssh-client"`
	SSHHosts  []string  `yaml:"ssh-hosts"`
	Whitelist []string  `yaml:"whitelist"`
}

func (c *Config) readConfig() {
	f, err := os.Open("config.yml")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(c)
	if err != nil {
		log.Fatal(err)
	}
}

type SSHResult struct {
	err    error
	result []string
	host   string
}

func SSHWorker(id int, conf SSHClient, sshhosts <-chan string, result chan<- SSHResult, verbose bool) {
	for sshhost := range sshhosts {
		if verbose {
			fmt.Println("Worker", id, "started working on", sshhost)
		}
		r := ProcessSSHHost(conf, sshhost)
		if verbose {
			fmt.Println("Worker", id, "finished working on", sshhost)
		}
		result <- r
	}
}

func ProcessSSHHost(conf SSHClient, sshhost string) SSHResult {
	var client *sshclient.Client
	var err error
	if len(conf.KeyFile) > 0 {
		client, err = sshclient.DialWithKey(sshhost+":22", conf.User, conf.KeyFile)
	} else {
		client, err = sshclient.DialWithPasswd(sshhost+":22", conf.User, conf.Pass)
	}
	if err != nil {
		return SSHResult{err: err, host: sshhost}
	}
	defer client.Close()

	out, err := client.Cmd("show configuration | display xml").Output()
	if err != nil {
		return SSHResult{err: err, host: sshhost}
	}

	doc, err := xmlquery.Parse(bytes.NewReader(out))
	if err != nil {
		return SSHResult{err: err, host: sshhost}
	}

	var ret []string
	for _, res := range xmlquery.Find(doc, "//*[@inactive and not(ancestor::*[@inactive])]") {
		ret = append(ret, sshhost+":"+GetPath(res))
	}
	return SSHResult{result: ret, host: sshhost}
}

type InactiveCache struct {
	db *badger.DB
}

func (c *InactiveCache) Open(file string) error {
	opts := badger.DefaultOptions(file)
	opts.Logger = nil
	db, err := badger.Open(opts)
	if err != nil {
		return err
	}
	c.db = db
	return nil
}

func (c *InactiveCache) Close() {
	c.db.Close()
}

func (c *InactiveCache) SetNow(inactive string) error {
	err := c.db.Update(func(txn *badger.Txn) error {
		timeNowBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(timeNowBytes, uint64(time.Now().Unix()))
		err := txn.Set([]byte(inactive), timeNowBytes)
		return err
	})
	return err
}

func (c *InactiveCache) Remove(inactive string) error {
	err := c.db.Update(func(txn *badger.Txn) error {
		err := txn.Delete([]byte(inactive))
		return err
	})
	return err
}

func (c *InactiveCache) GetAll() (map[string]time.Time, error) {
	ret := make(map[string]time.Time)
	err := c.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 10
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			k := item.Key()
			err := item.Value(func(v []byte) error {
				t := binary.BigEndian.Uint64(v)
				ret[string(k)] = time.Unix(int64(t), 0)
				return nil
			})
			if err != nil {
				return err
			}
		}

		return nil
	})
	return ret, err
}

func main() {
	var verbose bool
	var store bool
	flag.BoolVar(&verbose, "v", false, "More output needed")
	flag.BoolVar(&store, "s", false, "Use cache to determine stale entries (>30days)")
	flag.Parse()

	if verbose {
		store = true
	}

	var cfg Config
	cfg.readConfig()

	cache := InactiveCache{}
	err := cache.Open("database.db")
	if err != nil {
		log.Fatal(err)
	}
	defer cache.Close()

	//fmt.Printf("%v\n", cfg)

	sshhosts := make(chan string, len(cfg.SSHHosts))
	results := make(chan SSHResult, len(cfg.SSHHosts))

	for w := 1; w <= *cfg.SSHClient.NumWorkers; w++ {
		go SSHWorker(w, cfg.SSHClient, sshhosts, results, verbose)
	}

	for _, j := range cfg.SSHHosts {
		sshhosts <- j
	}
	close(sshhosts)

	older, err := cache.GetAll()
	if err != nil {
		log.Fatal(err)
	}

	touched := make(map[string]bool)
	whitelistMap := make(map[string]bool)

	for _, w := range cfg.Whitelist {
		whitelistMap[w] = true
	}

	for a := 1; a <= len(cfg.SSHHosts); a++ {
		result := <-results
		if result.err != nil {
			log.Fatalf("%s : %v\n", result.host, result.err)
		}
		for _, r := range result.result {
			if whitelistMap[r] {
				continue
			}
			if !store {
				fmt.Println(r)
			} else {
				touched[r] = true
				if prevTime, ok := older[r]; ok {
					diff := time.Since(prevTime)
					if diff.Hours() > 24*30 {
						if verbose {
							fmt.Println("STALE:", r, "diff:", diff.Round(time.Second))
						} else {
							fmt.Println(r)
						}
					} else {
						if verbose {
							fmt.Println("fresh:", r, "diff:", diff.Round(time.Second))
						}
					}
				} else {
					if verbose {
						fmt.Println("new:  ", r)
					}
					err = cache.SetNow(r)
					if err != nil {
						log.Fatal(err)
					}
				}
			}
		}
	}
	if store {
		for t := range touched {
			delete(older, t)
		}
		for o := range older {
			if verbose {
				fmt.Println("rem:  ", o)
			}
			err = cache.Remove(o)
			if err != nil {
				log.Fatal(err)
			}
		}
	}
}
