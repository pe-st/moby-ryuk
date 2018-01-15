package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

var port = flag.Int("p", 8080, "Port to bind at")

func main() {
	flag.Parse()
	log.Printf("Starting on port %d...", *port)

	cli, err := client.NewEnvClient()
	if err != nil {
		panic(err)
	}

	deathNote := make(map[string]bool)

	connected := make(chan bool)
	disconnected := make(chan bool)

	go func() {
		ln, _ := net.Listen("tcp", fmt.Sprintf(":%d", *port))
		for {
			conn, _ := ln.Accept()
			connected <- true
			reader := bufio.NewReader(conn)
			for {
				message, err := reader.ReadString('\n')

				if len(message) > 0 {
					query, err := url.ParseQuery(message)

					if err != nil {
						log.Println(err)
						continue
					}

					args := filters.NewArgs()
					for filterType, values := range query {
						for _, value := range values {
							args.Add(filterType, value)
						}
					}
					param, err := filters.ToParam(args)

					if err != nil {
						log.Println(err)
						continue
					}

					log.Printf("%+v\n", param)

					deathNote[param] = true

					conn.Write([]byte("ACK\n"))
				}

				if err != nil {
					log.Println(err)
					break
				}
			}
			disconnected <- true
			conn.Close()
		}
	}()

TimeoutLoop:
	for {
		select {
		case <-connected:
			log.Println("Connected")
		case <-disconnected:
			log.Println("Disconnected")
			select {
			case <-connected:
			case <-time.After(10 * time.Second):
				log.Println("Timed out waiting for connection")
				break TimeoutLoop
			}
		}
	}

	deletedContainers := make(map[string]bool)
	deletedNetworks := make(map[string]bool)
	deletedVolumes := make(map[string]bool)

	for param := range deathNote {
		log.Printf("Deleting %s\n", param)

		args, err := filters.FromParam(param)
		if err != nil {
			log.Println(err)
			continue
		}

		if containers, err := cli.ContainerList(context.Background(), types.ContainerListOptions{All: true, Filters: args}); err != nil {
			log.Println(err)
		} else {
			for _, container := range containers {
				cli.ContainerRemove(context.Background(), container.ID, types.ContainerRemoveOptions{RemoveVolumes: true, Force: true})
				deletedContainers[container.ID] = true
			}
		}

		networksPruneReport, err := cli.NetworksPrune(context.Background(), args)
		for _, networkID := range networksPruneReport.NetworksDeleted {
			deletedNetworks[networkID] = true
		}

		volumesPruneReport, err := cli.VolumesPrune(context.Background(), args)
		for _, volumeName := range volumesPruneReport.VolumesDeleted {
			deletedVolumes[volumeName] = true
		}
	}

	log.Printf("Removed %d container(s), %d network(s), %d volume(s)", len(deletedContainers), len(deletedNetworks), len(deletedVolumes))
}
