package main

import (
	"errors"
	"flag"
	"github.com/armon/consul-api"
	"github.com/samalba/dockerclient"
	"log"
	"time"
)

var consulAddress = flag.String("consul", "0.0.0.0:8500", "Address of consul server")
var dockerSock = flag.String("docker", "unix:///var/run/docker.sock", "Path to docker socket")

var docker *dockerclient.DockerClient
var catalog *consulapi.Catalog

// Callback used to listen to Docker's events
func eventCallback(event *dockerclient.Event, args ...interface{}) {
	switch event.Status {
	case "create":
	case "start":
		err := addcontainer(event.Id)
		if err != nil {
			log.Println("err:", err)
		}
	case "die":
		err := removecontainer(event.Id)
		if err != nil {
			log.Println("err:", err)
		}
	case "destroy":
	default:
		log.Printf("Received event: %#v\n", *event)
	}
}

func addcontainer(id string) error {
	// get our container details
	details, _ := docker.InspectContainer(id)
	if details.Name[1:] == "consul" {
		return errors.New("Not adding consul container")
	}
	//log.Printf("%#v\n", details.Name)
	// create a new registration object
	registration := new(consulapi.CatalogRegistration)
	// initalize it to our container
	registration.Node = details.Name[1:]
	registration.Address = details.NetworkSettings.IpAddress
	// Attempt to register it with consul
	log.Println("Adding container", details.Name[1:])
	_, err := catalog.Register(registration, nil)
	// Output any errors if we get them
	if err != nil {
		log.Println("err:", err)
		return err
	}
	return nil
}

func removecontainer(id string) error {
	// get our container details
	details, _ := docker.InspectContainer(id)
	// create a new registration object
	deregistration := new(consulapi.CatalogDeregistration)
	// initalize it to our container
	deregistration.Node = details.Name[1:]
	// Attempt to deregister it with consul
	log.Println("Removing container", details.Name[1:])
	_, err := catalog.Deregister(deregistration, nil)
	// Output any errors if we get them
	if err != nil {
		log.Println("err:", err)
		return err
	}
	return nil
}

func main() {
	// parse our cli flags
	flag.Parse()
	// Init the docker client
	// [todo] - error checking
	docker, _ = dockerclient.NewDockerClient(*dockerSock)

	// Get only running containers
	containers, err := docker.ListContainers(false)
	if err != nil {
		log.Fatal(err)
	}

	// Init the consul client
	consulConfig := consulapi.DefaultConfig()
	consulConfig.Address = *consulAddress
	consul, err := consulapi.NewClient(consulConfig)
	if err != nil {
		log.Fatal(err)
	}

	// Try to get our status object
	consulStatus := consul.Status()
	// Try to find our leader
	leader, err := consulStatus.Leader()
	// If we get an error
	if err != nil {
		log.Println("Error getting status:", err)
		// Look through the running containers for one named 'consul'
		for _, c := range containers {
			// If we find one
			if c.Names[0] == "/consul" {
				// Extract its IP
				details, _ := docker.InspectContainer(c.Id)
				// Update our client config
				consulConfig.Address = details.NetworkSettings.IpAddress + ":8500"
			}
		}
		if consulConfig.Address == *consulAddress {
			log.Fatal("Unable to determine consul address. Try using --consul or creating a container named 'consul'")
		}
		log.Println("Retrying with", consulConfig.Address)
		consul, _ = consulapi.NewClient(consulConfig)
		consulStatus = consul.Status()
		leader, err = consulStatus.Leader()
		if err != nil {
			log.Fatal(err)
		}
	}
	log.Println("Consul Leader is", leader)

	catalog = consul.Catalog()

	for _, c := range containers {
		// Get our container name
		container := c.Names[0][1:]
		// remove ugly leading slash
		// let the user know what's up
		log.Println("Found already running container:", container)
		err := addcontainer(c.Id)
		if err != nil {
			log.Println("err:", err)
		}
	}

	// Listen to events
	docker.StartMonitorEvents(eventCallback)
	// [todo] - figure out how to make this wait forever
	time.Sleep(3600 * time.Second)
}
