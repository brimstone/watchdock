package docker

import (
	"log"
)

type Processing struct {
}

func (self *Processing) Init(socket string) error {
	return nil
}

func (self *Processing) Sync(channel chan map[string]interface{}) {
	for {
		select {
		case event := <-channel:
			log.Println("Docker got a message %v", event)
		}
	}
}

func New(socket string) (*Processing, error) {
	self := new(Processing)
	err := self.Init(socket)
	if err != nil {
		return nil, err
	}
	return self, nil
}
