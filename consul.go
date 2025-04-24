package goconsul

import (
	"bytes"
	//"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	//"github.com/docker/docker/api/types"
	//"github.com/docker/docker/client"
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
	consulClient, err := api.NewClient(
		&api.Config{
			Address: consulAddress,
		})
	if err != nil {
		log.Fatal(err)
	}

	return &Service{
		consulClient: consulClient,
		id:           serviceID,
		name:         serviceName,
		address:      address,
		port:         port,
		tags:         tags,
	}
}

// Registers the service to consul and starts the basic TTL health check.
func (s *Service) Start(consulAddress, serviceAddr, handlerUrl /*, containerName*/ string) {
	var wg sync.WaitGroup
	wg.Add(1)
	s.ServiceIDCheck(&s.id, s.name)
	s.registerServiceConsul(serviceAddr)

	go s.updateHealthCheck()
	go s.WatchHealthChecks(consulAddress, handlerUrl)

	startPerformanceChecks() //containerName)
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
	watchParams := map[string]interface{}{
		"type":    "service",
		"service": s.name,
	}

	plan, err := watch.Parse(watchParams)
	if err != nil {
		log.Printf("Failed to create watcher for %s: %v", s.id, err)
		return
	}

	//HTTP handler for consul watcher doesnt work, while
	//the function handler does - thats why the http post request is sent by hand
	plan.Handler = func(idx uint64, data interface{}) {
		entries := data.([]*api.ServiceEntry)
		for _, entry := range entries {
			if entry.Service.ID != s.id {
				continue
			}

			for _, check := range entry.Checks {
				// log.Printf("Health check %s for service %s is %s",
				// 	check.CheckID, s.id, check.Status)

				if check.Status == api.HealthCritical {
					log.Printf("CRITICAL: Health check %s failed for service %s", check.CheckID, s.id)
					//sends the post request to the module
					postBody, _ := json.Marshal(map[string]string{
						"ServiceID":     s.id,
						"CheckName":     check.CheckID,
						"CheckType":     check.Type,
						"CheckStatus":   check.Status,
						"FailureTime":   time.Now().GoString(),
						"FailureOutput": check.Output,
					})
					responseBody := bytes.NewBuffer(postBody)
					resp, err := http.Post(handlerURL, "application/json", responseBody)
					if err != nil {
						log.Fatalf("An Error Occured %v", err)
					}
					defer resp.Body.Close()
					body, err := io.ReadAll(resp.Body)
					if err != nil {
						log.Fatalln(err)
					}
					sb := string(body)
					log.Println(sb)
				}
			}
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

func startPerformanceChecks() { //containerName string) {
	// cli, err := client.NewClientWithOpts(client.FromEnv)
	// if err != nil {
	// 	panic(err)
	// }

	// stats, err := cli.ContainerStats(context.Background(), containerName, false)
	// if err != nil {
	// 	panic(err)
	// }
	// defer stats.Body.Close()

	// data, err := io.ReadAll(stats.Body)
	// if err != nil {
	// 	panic(err)
	// }

	// var statsJSON types.StatsJSON
	// err = json.Unmarshal(data, &statsJSON)
	// if err != nil {
	// 	panic(err)
	// }

	// cpuDelta := float64(statsJSON.CPUStats.CPUUsage.TotalUsage) - float64(statsJSON.PreCPUStats.CPUUsage.TotalUsage)
	// systemDelta := float64(statsJSON.CPUStats.SystemUsage) - float64(statsJSON.PreCPUStats.SystemUsage)
	// numberOfCores := float64(statsJSON.CPUStats.OnlineCPUs)
	// cpuPercent := (cpuDelta / systemDelta) * numberOfCores * 100.0

	// memUsage := float64(statsJSON.MemoryStats.Usage) / (1024 * 1024) // in MB
	// memLimit := float64(statsJSON.MemoryStats.Limit) / (1024 * 1024) // in MB
	// memPercent := (memUsage / memLimit) * 100.0

	// fmt.Printf("CPU usage: %.2f%%\n", cpuPercent)
	// fmt.Printf("Memory usage: %.2f MB / %.2f MB (%.2f%%)\n", memUsage, memLimit, memPercent)

	usage, _ := os.ReadFile("/sys/fs/cgroup/memory/memory.usage_in_bytes")
	fmt.Println("Memory usage (bytes):", string(usage))

	cpu, _ := os.ReadFile("/sys/fs/cgroup/cpu/cpuacct.usage")
	fmt.Println("CPU usage (nanoseconds):", string(cpu))

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
