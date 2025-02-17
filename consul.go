package goconsul

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
)

const (
	ttl     = time.Second * 8
	checkID = "check_health"
)

type Service struct {
	consulClient *api.Client
	id           string
	name         string
	address      string
	port         int
}

//Registers a new consul client based on provided arguments
//Returns a Service struct with a pointer to the consul client 
func NewService(serviceID string, serviceName string, address string, port int) *Service {
	client, err := api.NewClient(
		&api.Config{})
	if err != nil {
		log.Fatal(err)
	}
	return &Service{
		consulClient: client,
		id:           serviceID,
		name:         serviceName,
		address:      address,
		port:         port,
	}
}

func (s *Service) Start() {
	var wg sync.WaitGroup
	wg.Add(1)
	s.registerServiceConsul()

	go s.updateHealthCheck()

	defer wg.Done()
	wg.Wait()
}

func (s *Service) updateHealthCheck() {
	ticker := time.NewTicker(time.Second * 5)
	for {
		err := s.consulClient.Agent().UpdateTTL(checkID, "online", api.HealthPassing)
		if err != nil {
			log.Fatal(err)
		}
		<-ticker.C
		fmt.Printf("Service TTL updated! \n")
	}

}

func (s *Service) registerServiceConsul() {

	register := &api.AgentServiceRegistration{
		ID:      s.id,
		Name:    s.name,
		Tags:    []string{"test"},
		Address: s.address,
		Port:    s.port,
		Check: &api.AgentServiceCheck{
			DeregisterCriticalServiceAfter: ttl.String(),
			TLSSkipVerify:                  true,
			TTL:                            ttl.String(),
			CheckID:                        checkID,
		},
	}

	if err := s.consulClient.Agent().ServiceRegister(register); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Service - '%v' registered! \n", s.id)
}

func (s *Service) ServiceDiscovery(serviceName string) {
	services, _, err := s.consulClient.Health().Service(serviceName, "", true, nil)
	if err != nil {
		log.Fatal(err)
	}

	for _, entry := range services {
		fmt.Printf("Service: %s | Address: %s | Port: %d \n",
			entry.Service.Service, entry.Node.Address, entry.Service.Port)
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
