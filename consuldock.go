// Dynamic Consul Node/service creation based on docker containers
package main

import (
	"errors"
	"flag"
	"github.com/armon/consul-api"
	//"github.com/davecgh/go-spew/spew"
	"github.com/samalba/dockerclient"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

var consulAddress = flag.String("consul", "0.0.0.0:8500", "Address of consul server")
var dockerSock = flag.String("docker", "unix:///var/run/docker.sock", "Path to docker socket")

var docker *dockerclient.DockerClient
var catalog *consulapi.Catalog

var containers map[string]Container

type Service struct {
	Name     string
	Port     int
	Protocol string
	Status   string
	CheckID  string
	Output   string
}

type Container struct {
	Id       string
	Name     string
	Address  string
	Services []Service
}

func (c Container) CheckAll() {
	for i, _ := range c.Services {
		// only support simple tcp syn checks right now
		c.Services[i].CheckID = "TCP SYN"
		// build our address with our port
		address := c.Address + ":" + strconv.Itoa(c.Services[i].Port)
		// Start the clock
		starttime := time.Now()
		// Actually try to open a connection for one second
		conn, err := net.DialTimeout("tcp", address, time.Second)
		endtime := time.Now()
		// If we can't, log an error to consul as well
		if err != nil {
			if c.Services[i].Status != "critical" {
				log.Printf("Service %s:%s [%s] has error %s\n", c.Name, c.Services[i].Name, address, err)
			}
			// [todo] - Multiple services on a node can't have different states
			c.Services[i].Output = "Error: " + err.Error()
			c.Services[i].Status = "critical"
		} else {
			if c.Services[i].Status == "unknown" {
				log.Printf("Service %s:%s [%s] passing\n", c.Name, c.Services[i].Name, address)
			} else if c.Services[i].Status == "critical" {
				log.Printf("Service %s:%s [%s] recovered\n", c.Name, c.Services[i].Name, address)
			}
			c.Services[i].Status = "passing"
			c.Services[i].Output = "Successful SYN. Connect time: " + endtime.Sub(starttime).String()
			// close our socket
			conn.Close()
		}
	}
	c.Register()
}

func addContainer(id string) (*Container, error) {
	// get our container details
	details, _ := docker.InspectContainer(id)
	if details.Name[1:] == "consul" {
		return nil, errors.New("Not adding consul container")
	}
	// Create a new container object
	c := Container{Id: id, Name: details.Name[1:], Address: details.NetworkSettings.IpAddress}

	log.Println("Adding container", c.Name)

	if len(details.Config.ExposedPorts) > 0 {
		// Loop though the exposed ports and register each of them as services to consul
		for portraw, _ := range details.Config.ExposedPorts {
			// Split apart our port string from docker
			port := strings.Split(portraw, "/")
			intport, _ := strconv.Atoi(port[0])
			serviceName := c.Name
			for _, envVar := range details.Config.Env {
				envVarParts := strings.Split(envVar, "=")
				envVarPartsService := strings.Split(envVarParts[0], "_")
				if envVarPartsService[0] == "SERVICE" {
					if envVarPartsService[1] == "NAME" && serviceName == "" {
						serviceName = envVarParts[1]
					}
					if envVarPartsService[1] == port[0] {
						serviceName = envVarParts[1]
					}
				}
			}
			// [todo] - Figure out the right service name
			// 1. Check for the env variable SERVICE_{Port}_NAME
			// 2. Check for the env variable SERVICE_NAME
			// 3. Use the container name
			c.Services = append(c.Services, Service{Port: intport, CheckID: "TCP SYN", Status: "unknown", Name: serviceName})
		}
	}

	containers[c.Name] = c

	return &c, nil
}

func (c Container) Register() error {
	// create a new registration object
	registration := new(consulapi.CatalogRegistration)
	// initalize it with our container details
	registration.Node = c.Name
	registration.Address = c.Address

	if len(c.Services) > 0 {
		// Loop though the exposed ports and register each of them as services to consul
		for _, containerService := range c.Services {

			// Create a new Service object
			service := new(consulapi.AgentService)
			// Name our service something unique
			service.Service = containerService.Name
			// Convert the port to an integer
			service.Port = containerService.Port
			// Add tags to the service
			service.Tags = []string{"consuldock"}
			// Bind the service to our registration object
			registration.Service = service

			// Create a new agent service check
			check := new(consulapi.AgentCheck)
			// Define the status of our check
			check.Status = containerService.Status // or Lastly, the status must be one of "unknown", "passing", "warning", or "critical"
			// Add the check id
			check.CheckID = containerService.CheckID
			// Add the output of the check
			check.Output = containerService.Output
			// Add ntoes to the check
			check.Notes = "consuldock managed node"
			// Add our Service Check to our registration object
			registration.Check = check

			// Attempt to register our node with service
			_, err := catalog.Register(registration, nil)
			// Output any errors if we get them
			if err != nil {
				log.Println("err:", err)
				return err
			}
		}
	}

	// Attempt to register our node with service
	_, err := catalog.Register(registration, nil)
	// Output any errors if we get them
	if err != nil {
		log.Println("err:", err)
		return err
	}

	// Attempt to register it with consul
	return nil
}

func removeContainer(id string) error {
	// find our container with this id
	for i, container := range containers {
		if container.Id == id {
			// [todo] - Add error checking on container deregistration
			err := container.Deregister()
			// Output any errors if we get them
			if err != nil {
				log.Println("err:", err)
				return err
			}
			delete(containers, i)
		}
	}
	return nil
}

func (c Container) Deregister() error {
	// create a new registration object
	deregistration := new(consulapi.CatalogDeregistration)
	// initalize it to our container
	deregistration.Node = c.Name
	// Attempt to deregister it with consul
	log.Println("Removing container", c.Name)
	_, err := catalog.Deregister(deregistration, nil)
	// Output any errors if we get them
	if err != nil {
		log.Println("err:", err)
		return err
	}
	return nil
}

// Callback used to listen to Docker's events
func eventCallback(event *dockerclient.Event, args ...interface{}) {
	switch event.Status {
	case "create":
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
	default:
		log.Printf("Received event: %#v\n", *event)
	}
}

func main() {
	containers = make(map[string]Container)

	// Function level variables
	var err error

	// parse our cli flags
	flag.Parse()

	// Init the docker client
	docker, err = dockerclient.NewDockerClient(*dockerSock, nil)
	if err != nil {
		log.Fatal(err)
	}

	// Get only running containers
	runningContainers, err := docker.ListContainers(false)
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
		for _, c := range runningContainers {
			// If we find one
			for _, name := range c.Names {
				names := strings.Split(name, "/")
				if names[1] == "consul" {
					// Extract its IP
					details, _ := docker.InspectContainer(c.Id)
					// Update our client config
					consulConfig.Address = details.NetworkSettings.IpAddress + ":8500"
				}
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
	// let the users know we found the leader
	log.Println("Consul Leader is", leader)

	// Get an object to our catalog
	catalog = consul.Catalog()

	// Add nodes for each container we're running
	for _, c := range runningContainers {
		// Get our container name
		container := c.Names[0][1:]
		// remove ugly leading slash
		// let the user know what's up
		log.Println("Found already running container:", container)
		mycontainer, err := addContainer(c.Id)
		if err != nil {
			log.Println("Error adding container:", err)
		} else {
			mycontainer.Deregister()
			mycontainer.Register()
		}
	}

	// Remove nodes marked by us earlier that aren't running any longer

	// Get a list of the nodes in consul
	nodes, _, err := catalog.Nodes(nil)
	if err != nil {
		log.Println("Error getting list of nodes:", err)
	}
	for _, nodeRef := range nodes {
		node, _, err := catalog.Node(nodeRef.Node, nil)
		if err != nil {
			log.Println("Error getting data for node", nodeRef.Node, ":", err)
		}
		// Look for the container in docker
		for _, serviceRef := range node.Services {
			for _, tagName := range serviceRef.Tags {
				// If this container was put here by us, remove it
				// [todo] - What am I doing here?
				if tagName == "consuldock" {
					mycontainer := Container{Name: nodeRef.Node}
					mycontainer.Deregister()
				}
			}
		}
	}
	// Since we didn't find it, ok to remove

	log.Println("Finished enumerating containers, starting watch for docker events.")
	// Listen to events
	docker.StartMonitorEvents(eventCallback)
	// Periodically check on our services, forever
	for {
		for i, _ := range containers {
			go containers[i].CheckAll()
		}
		time.Sleep(2 * time.Second)
	}
}
