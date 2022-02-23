package main

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/antchfx/xmlquery"
	"io/ioutil"
	"log"
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

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		log.Fatal("missing file as command argument")
	}

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
