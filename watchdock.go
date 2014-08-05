// Dynamic Consul Node/service creation based on docker containers
package main

import (
	"errors"
	"flag"
	"github.com/armon/consul-api"
	//"github.com/davecgh/go-spew/spew"
	"github.com/samalba/dockerclient"
	"log"
	"time"
)

var consulAddress = flag.String("consul", "0.0.0.0:8500", "Address of consul server")
var dockerSock = flag.String("docker", "unix:///var/run/docker.sock", "Path to docker socket")

var docker *dockerclient.DockerClient

// Callback used to listen to Docker's events
func eventCallback(event *dockerclient.Event, args ...interface{}) {
	switch event.Status {
	/*case "create":
	case "start":
		c, err := addContainer(event.Id)
		c.Register()
		if err != nil {
			log.Println("err:", err)
		}
	case "die":
		err := removeContainer(event.Id)
		if err != nil {
			log.Println("err:", err)
		}
	case "destroy":
	case "delete":
	*/
	default:
		log.Printf("Received event: %#v\n", *event)
	}
}

func findContainer(name string, running bool) (*dockerclient.ContainerInfo, error) {
	runningContainers, err := docker.ListContainers(!running)
	if err != nil {
		log.Fatal(err)
	}
	for _, c := range runningContainers {
		// If we find one
		if c.Names[0] == "/consul" {
			return docker.InspectContainer(c.Id)
		}
	}
	return nil, errors.New("Not found")
}

func runContainer(name string, image string, tag string) (*dockerclient.ContainerInfo, error) {
	log.Println("Creating a container", image)
	log.Println("Pulling container", image)
	docker.PullImage(image, tag)
	// We didn't find a consul container, create one
	consulContainerConfig := &dockerclient.ContainerConfig{
		Image: image,
		Cmd: []string{
			"--bootstrap-expect", "2",
		},
	}
	_, err := docker.CreateContainer(consulContainerConfig, name)
	if err != nil {
		// [todo] - if error is Not found, pull down the image and try creating the container again
		return nil, err
	}
	consulContainer, err := findContainer("/consul", false)
	if err != nil {
		return nil, err
	}
	err = docker.StartContainer(consulContainer.Id, nil)
	if err != nil {
		return nil, err
	}
	return consulContainer, nil
}

func main() {
	// Function level variables
	var err error

	// parse our cli flags
	flag.Parse()

	// Init the docker client
	docker, err = dockerclient.NewDockerClient(*dockerSock, nil)
	if err != nil {
		log.Fatal(err)
	}

	// Look for an existing consul container
	log.Println("Looking for existing consul container")
	consulContainer, err := findContainer("/consul", false)
	if err != nil {
		if err.Error() != "Not found" {
			log.Fatal(err)
		}
		consulContainer, err = runContainer("consul", "brimstone/consul", "latest")
	}
	// [consider] - purge the old one, really shake the boat

	// Now we have a container
	// If it's not running
	if !consulContainer.State.Running {
		// start it
		err = docker.StartContainer(consulContainer.Id, nil)
		if err != nil {
			log.Fatal(err)
		}
	}
	// get its IP
	consulConfig := consulapi.DefaultConfig()
	consulConfig.Address = consulContainer.NetworkSettings.IpAddress + ":8500"
	log.Println("Found consul at", consulConfig.Address)

	// Establish our client
	consul, _ := consulapi.NewClient(consulConfig)
	consulStatus := consul.Status()

	// Find our leader so the user knows we've connected right
	leader, err := consulStatus.Leader()
	for err != nil {
		log.Println("Warning: ", err)
		time.Sleep(time.Second)
		leader, err = consulStatus.Leader()
	}
	for leader == "" && err == nil {

		log.Println("No leader and no error, waiting for a valid leader")
		leader, err = consulStatus.Leader()
		time.Sleep(2 * time.Second)
	}
	// let the users know we found the leader
	log.Println("Consul leader is", leader)

	/*
		log.Println("Finished enumerating containers, starting watch for docker events.")
		// Listen to events
		docker.StartMonitorEvents(eventCallback)
		// Periodically check on our services, forever
		time.Sleep(2 * time.Hour)
	*/
}
