package main

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/antchfx/xmlquery"
	"github.com/helloyi/go-sshclient"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"os"
	"strings"
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

//func SSHWorker(id int, conf SSHClient, sshhosts <-chan string, result chan<- []string) {
//	for sshhost := range sshhosts {
//
//	}
//}

func ProcessSSHHost(conf SSHClient, sshhost string) ([]string, error) {
	var client *sshclient.Client
	var err error
	if len(conf.KeyFile) > 0 {
		client, err = sshclient.DialWithKey(sshhost+":22", conf.User, conf.KeyFile)
	} else {
		client, err = sshclient.DialWithPasswd(sshhost+":22", conf.User, conf.Pass)
	}
	if err != nil {
		return nil, err
	}
	defer client.Close()

	out, err := client.Cmd("show configuration | display xml").Output()
	if err != nil {
		log.Fatal(err)
	}

	doc, err := xmlquery.Parse(bytes.NewReader(out))
	if err != nil {
		log.Fatal(err)
	}

	var ret []string
	for _, res := range xmlquery.Find(doc, "//*[@inactive and not(ancestor::*[@inactive])]") {
		ret = append(ret, sshhost+":"+GetPath(res))
	}
	return ret, nil
}

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		log.Fatal("missing file as command argument")
	}

	var cfg Config
	cfg.readConfig()

	fmt.Printf("%v\n", cfg)

	for _, filename := range args {
		test, err := ioutil.ReadFile(filename)
		if err != nil {
			log.Fatal(err)
		}

		doc, err := xmlquery.Parse(bytes.NewReader(test))
		if err != nil {
			log.Fatal(err)
		}

		for _, n := range xmlquery.Find(doc, "//*[@inactive and not(ancestor::*[@inactive])]") {
			fmt.Println(GetPath(n))
		}

	}
}
