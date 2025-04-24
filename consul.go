package goconsul

import (
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/api/watch"
)

const (
	ttl     = time.Second * 8
	checkID = "check"
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
func (s *Service) Start(consulAddress, serviceAddr, handlerUrl string) {
	var wg sync.WaitGroup
	wg.Add(1)
	s.ServiceIDCheck(&s.id, s.name)
	s.registerServiceConsul(serviceAddr)

	go s.updateHealthCheck()
	go s.WatchHealthChecks(consulAddress, handlerUrl)

	wg.Wait()
}

// Registers the service to consul.
func (s *Service) registerServiceConsul(serviceAddr string) {
	var httpEndpoint, tcpAddress string

	if strings.HasPrefix(serviceAddr, "http") {
		httpEndpoint = serviceAddr
		parsedURL, err := url.Parse(serviceAddr)
		if err != nil {
			log.Printf("Warning: Could not parse URL %s: %v", serviceAddr, err)
			tcpAddress = s.address + ":" + strconv.Itoa(s.port)
		} else {
			tcpAddress = parsedURL.Host
		}
	} else {
		httpEndpoint = fmt.Sprintf("http://%s:%d%s", s.address, s.port, serviceAddr)
		tcpAddress = fmt.Sprintf("%s:%d", s.address, s.port)
	}

	CheckTTL := &api.AgentServiceCheck{
		CheckID:                checkID + "_TTL_" + s.id,
		TLSSkipVerify:          true,
		TTL:                    ttl.String(),
		FailuresBeforeWarning:  1,
		FailuresBeforeCritical: 1,
	}
	CheckHTTP := &api.AgentServiceCheck{
		CheckID:                checkID + "_HTTP_" + s.id,
		HTTP:                   httpEndpoint,
		TLSSkipVerify:          true,
		Method:                 "GET",
		Interval:               "30s",
		Status:                 "passing",
		FailuresBeforeWarning:  1,
		FailuresBeforeCritical: 1,
	}
	CheckTCP := &api.AgentServiceCheck{
		CheckID:                checkID + "_TCP_" + s.id,
		TCP:                    tcpAddress,
		TLSSkipVerify:          true,
		Interval:               "30s",
		Status:                 "passing",
		Timeout:                "5s",
		FailuresBeforeWarning:  1,
		FailuresBeforeCritical: 1,
	}

	register := &api.AgentServiceRegistration{
		ID:      s.id,
		Name:    s.name,
		Tags:    s.tags,
		Address: s.address,
		Port:    s.port,
		Checks:  []*api.AgentServiceCheck{CheckTTL, CheckHTTP, CheckTCP},
	}

	if err := s.consulClient.Agent().ServiceRegister(register); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Service - '%v' registered! \n", s.id)
}

// Updates the service's health/TTL.
func (s *Service) updateHealthCheck() {
	checkId := checkID + "_TTL_" + s.id
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

func (s *Service) WatchHealthChecks(consulAddress, handlerURL string) {

	config := watch.HttpHandlerConfig{
		Path:          handlerURL,
		Method:        "POST",
		TLSSkipVerify: true,
		Header: map[string][]string{
			"Content-Type": {"application/json"},
		},
	}

	watchParams := map[string]interface{}{
		"type":                "service",
		"service":             s.name,
		"handler_type":        "http",
		"http_handler_config": config,
	}

	//plan, err := watch.Parse(watchParams)
	var exemptList []string
	plan, err := watch.ParseExempt(watchParams, exemptList)
	if err != nil {
		log.Printf("Failed to create watcher for %s: %v", s.id, err)
		return
	}
	if len(exemptList) > 0 {
		for i := 0; i < len(exemptList); i++ {
			log.Printf(exemptList[i])
		}
	}

	go func() {
		err := plan.Run(consulAddress)
		if err != nil {
			log.Printf("Watcher for %s failed: %v", s.id, err)
		}
	}()

	log.Printf("Started health check watcher for service %s", s.id)
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
func (s *Service) ServiceIDCheck(id *string, name string) {
	filterString := fmt.Sprintf("Service == %s", name)
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
