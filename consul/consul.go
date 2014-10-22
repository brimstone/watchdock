package consul

import (
	//"encoding/json"
	//"github.com/davecgh/go-spew/spew"
	//"io/ioutil"
	"log"
	//"os"
	//"path"
	//"strings"
	//"time"
	"github.com/armon/consul-api"
)

func logit(v ...interface{}) {
	log.Println("Consul:", v)
}

type Consul struct {
	catalog *consulapi.Catalog
	client  *consulapi.Client
}

func (consul *Consul) Init(connect string) error {
	var err error
	err = nil
	consulConfig := consulapi.DefaultConfig()
	consulConfig.Address = connect
	consul.client, err = consulapi.NewClient(consulConfig)
	return err
}

func (consul *Consul) Sync(readChannel <-chan map[string]interface{}, writeChannel chan<- map[string]interface{}) {
}

func New(connect string) (*Consul, error) {
	consul := new(Consul)
	err := consul.Init(connect)
	if err != nil {
		return nil, err
	}
	return consul, nil
}
