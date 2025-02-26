package goconsul

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
)

const (
	ttl     = time.Second * 8
	checkID = "check_TTL"
)

type Service struct {
	consulClient *api.Client
	id           string
	name         string
	address      string
	port         int
	tags         []string
}

// Registers a new consul client based on provided arguments.
// Returns a Service struct with a pointer to the consul client.
func NewService(consulAddress string, serviceID string, serviceName string, address string, port int, tags []string) *Service {
	client, err := api.NewClient(
		&api.Config{
			Address: consulAddress,
		})
	if err != nil {
		log.Fatal(err)
	}

	return &Service{
		consulClient: client,
		id:           serviceID,
		name:         serviceName,
		address:      address,
		port:         port,
		tags:         tags,
	}
}

// Registers the service to consul and starts the basic TTL health check.
func (s *Service) Start() {
	var wg sync.WaitGroup
	wg.Add(1)
	s.ServiceIDCheck(&s.id)
	s.registerServiceConsul()

	go s.updateHealthCheck()

	defer wg.Done()
	wg.Wait()
}

// Registers the service to consul.
func (s *Service) registerServiceConsul() {

	register := &api.AgentServiceRegistration{
		ID:      s.id,
		Name:    s.name,
		Tags:    s.tags,
		Address: s.address,
		Port:    s.port,
		Check: &api.AgentServiceCheck{
			TLSSkipVerify: true,
			TTL:           ttl.String(),
			CheckID:       checkID + "_" + s.id,
		},
	}

	if err := s.consulClient.Agent().ServiceRegister(register); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Service - '%v' registered! \n", s.id)
}

// Updates the service's health/TTL.
func (s *Service) updateHealthCheck() {
	checkId := checkID + "_" + s.id
	ticker := time.NewTicker(time.Second * 5)
	for {
		err := s.consulClient.Agent().UpdateTTL(checkId, "Service TTL updated", api.HealthPassing)
		if err != nil {
			log.Fatal(err)
		}
		<-ticker.C
		fmt.Printf("Service TTL updated! \n")
	}

}

// doesnt make sense - docker already notifies if port is in use
func (s *Service) ServiceAddressCheck(port *int) {
	services, err := s.consulClient.Agent().Services()
	if err != nil {
		log.Fatal(err)
	}
	for _, entry := range services {
		if *port == entry.Port {
			*port++
			fmt.Printf("Port - %v already in use, changed to - %v \n", entry.Port, *port)
		}
	}

}

// testing if automating a pick for service ID is worth it
// method would check if service id exists and change it to something else
func (s *Service) ServiceIDCheck(id *string) {
	filterString := fmt.Sprintf("ID contains %s", *id)
	services, err := s.consulClient.Agent().ServicesWithFilter(filterString)
	if err != nil {
		log.Fatal(err)
	}
	baseValue := 1
	for _, entry := range services {
		if *id == entry.ID {
			*id = strings.Replace(*id, strconv.Itoa(baseValue), strconv.Itoa(baseValue+1), 1)
			fmt.Printf("ID - %s already in use, changed to - %s \n", entry.ID, *id)
		}
		baseValue++
	}

}

func (s *Service) ServiceDiscovery() {
	services, err := s.consulClient.Agent().Services()
	if err != nil {
		log.Fatal(err)
	}

	for _, entry := range services {
		fmt.Printf("Service: %s | Address: %s | Port: %d \n",
			entry.ID, entry.Address, entry.Port)
	}
}

func (s *Service) ServiceCatalog() {
	services /*meta*/, _, err := s.consulClient.Catalog().Services(nil)
	if err != nil {
		log.Fatal(err)
	}

	// Print the retrieved services
	fmt.Println("Services registered in Consul:")
	for service, tags := range services {
		fmt.Printf("- %s (Tags: %v)\n", service, tags)
	}

	// Print metadata (useful for debugging)
	//fmt.Printf("Query Metadata: %+v\n", meta)

}
