// Dynamic Consul Node/service creation based on docker containers
package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/armon/consul-api"
	//"github.com/davecgh/go-spew/spew"
	dockerclient "github.com/fsouza/go-dockerclient"
	"log"
	"strconv"
	"strings"
	"time"
)

// Define our global variables
var docker *dockerclient.Client

var consul *consulapi.Client
var consulInstance *dockerclient.Container
var consulContainer Container

/*
// Callback used to listen to Docker's events
func dockerCallback(event *dockerclient.Event, args ...interface{}) {
	log.Println("Detected docker", event.Status, "event")
	switch event.Status {
	case "die":
		if event.Id == consulInstance.Id {
			consulInstance, consul = findConsul(consulContainer)
		}
	case "destroy":
	case "delete":
	// [todo] - Make and call findContainerById(event.Id)
	default:
		log.Printf("Received event: %#v\n", *event)
	}
}
*/

func findConsul(consulContainer Container) (*dockerclient.Container, *consulapi.Client) {
	// Find our container
	consulInstance, err := startInstance(consulContainer)
	if err != nil {
		log.Fatal("findConsul: ", err)
	}
	// get its IP
	// [todo] - handle a blank ip address
	consulConfig := consulapi.DefaultConfig()
	consulConfig.Address = consulInstance.NetworkSettings.IPAddress + ":8500"
	log.Println("Found consul at", consulConfig.Address)

	// Establish our client
	consul, _ := consulapi.NewClient(consulConfig)
	return consulInstance, consul
}

func mapKVPairs() *map[string]Container {
	kv := consul.KV()

	pairs, _, _ := kv.List("containers", nil)

	containers := make(map[string]Container)
	// This will basically loop through every container item under in our pair
	for i := len(pairs) - 1; i >= 0; i-- {
		// Figure out how many levels are in our key
		levels := strings.Split(pairs[i].Key, "/")
		// we don't care about the top level keys
		if len(levels) < 3 || levels[2] == "" {
			continue
		}
		// extract our container, there's probably a better way
		container, _ := containers[levels[1]]
		container.Name = levels[1]
		// convert our KV value to a real string
		value := string(pairs[i].Value)
		// big switch that unmarshalls the KV into a Container
		switch levels[2] {
		case "image":
			container.Image = value
			//case "ports":
			//container.Ports = strings.Split(value, ",")
		case "hostname":
			container.Hostname = value
		case "maxinstances":
			container.MaxInstances, _ = strconv.Atoi(value)
		//case "volumes":
		//container.Volumes = strings.Split(value, ",")
		case "volumesfrom":
			container.VolumesFrom = strings.Split(value, ",")
		case "where":
			container.Where = strings.Split(value, ",")
		case "pty":
			container.Pty = value == "true"
		}
		// finally set the container back
		containers[levels[1]] = container
	}
	// return it to our calling function
	return &containers
}

func waitForLeader() string {
	leader := ""
	var err error
	// While we don't have a leader
	for leader == "" {
		log.Println("Looking for consul leader")
		consulInstance, consul = findConsul(consulContainer)
		consulStatus := consul.Status()

		// Find our leader so the user knows we've connected right
		leader, err = consulStatus.Leader()
		log.Println("leader is", leader)
		// If we have an error getting the leader, wait a second, then try again
		startTime := time.Now()
		for err != nil && time.Since(startTime) < time.Minute {
			log.Println("Warning: ", err)
			time.Sleep(time.Second)
			leader, err = consulStatus.Leader()
		}
		// Remember when we started waiting for leader election to happen
		startTime = time.Now()
		// break if we get leader, an error, or it times out
		for leader == "" && err == nil && time.Since(startTime) < time.Minute {
			log.Println("No leader and no error, waiting for a valid leader")
			leader, err = consulStatus.Leader()
			time.Sleep(2 * time.Second)
			// check to see if we're joined to all of our known nodes
			for _, x := range otherConsul {
				agent := consul.Agent()
				// get all of the members of our consul instance
				members, err := agent.Members(false)
				if err != nil {
					log.Fatal(err)
				}
				flag := false
				// loop through them
				for _, member := range members {
					// if we're already connected, don't try to reconnect
					if member.Addr == x {
						flag = true
						break
					}
				}
				// since we didn't set off our flag, connect to our instance
				if !flag {
					agent.Join(x, false)
				}
			}
		}
		// If we still don't have a leader, than we timed out
		if leader == "" {
			log.Println("Timeout while waiting on leader election, killing the container")
			docker.StopContainer(consulInstance.ID, 0)
			docker.RemoveContainer(dockerclient.RemoveContainerOptions{ID: consulInstance.ID})
		}
	}

	// let the users know we found the leader
	log.Println("Consul leader is", leader)

	return leader
}

func main() {
	// Function level variables
	var err error

	// parse our cli flags
	var dockerSock = flag.String("docker", "unix:///var/run/docker.sock", "Path to docker socket")
	flag.Var(&otherConsul, "join", "Clients to join")
	flag.Parse()

	// Init the docker client
	docker, err = dockerclient.NewClient(*dockerSock)
	if err != nil {
		log.Fatal(err)
	}

	// TODO Maybe make this its own function?
	// Here's where we define our consul container
	// Build our consul cmd line from our options
	cmd := []string{
		"--bootstrap-expect", strconv.Itoa(len(otherConsul) + 1),
	}
	consulContainer.Cmd = cmd
	consulContainer.Name = "consul"
	consulContainer.Image = "brimstone/consul"
	consulContainer.Ports = make(map[dockerclient.Port][]dockerclient.PortBinding)
	consulContainer.Ports["8500/tcp"] = []dockerclient.PortBinding{
		dockerclient.PortBinding{
			HostIp:   "0.0.0.0",
			HostPort: "8500",
		},
	}
	consulContainer.Ports["8301/tcp"] = []dockerclient.PortBinding{
		dockerclient.PortBinding{
			HostIp:   "0.0.0.0",
			HostPort: "8301",
		},
	}
	consulContainer.Volumes = make(map[string]struct{})
	consulContainer.Env = make([]string, 0)

	log.Println("Finished enumerating containers, starting watch for docker events.")
	// Listen to events
	// [fixme] - docker.StartMonitorEvents(dockerCallback)
	// Periodically check on our services, forever
	for {
		// Wait for our leader
		waitForLeader()
		// Gather up all of the containers we should now about
		containers = *mapKVPairs()

		// pull down all of the images
		pullContainers(containers)
		pullContainer(consulContainer)
		pullContainer(Container{Image: "brimstone/watchdock"})

		// clean up dead containers
		cleanUntaggedContainers()
		// clean up untagged images
		cleanImages()
		// start what's not running
		startContainers(containers)

		// TODO add what's running to our internal structure
		// TODO sync what we're running with consul

		// sleep for a bit
		time.Sleep(2 * time.Minute)
		// make sure our consul container is running
		consulInstance, consul = findConsul(consulContainer)
		for _, x := range otherConsul {
			agent := consul.Agent()
			agent.Join(x, false)
		}
	}
}
