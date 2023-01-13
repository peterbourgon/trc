package eztrc

import (
	"fmt"
	"net/http"

	"github.com/peterbourgon/trc/trctrace"
	"github.com/peterbourgon/trc/trctrace/trctracehttp"
)

func Handler() http.Handler {
	return trctracehttp.NewServer2(Collector())
}

func PeersHandler(urls ...string) http.Handler {
	localTarget := trctracehttp.NewTarget("local", Collector())
	otherTargets := make([]*trctracehttp.Target, len(urls))
	for i := range urls {
		name := urls[i]
		searcher := trctracehttp.NewClient(http.DefaultClient, urls[i])
		otherTargets[i] = trctracehttp.NewTarget(name, searcher)
	}
	server, err := trctracehttp.NewServer(trctracehttp.ServerConfig{
		Local: localTarget,
		Other: otherTargets,
	})
	if err != nil {
		panic(err)
	}
	return server
}

func GroupsHandler(groups map[string][]string) http.Handler {
	localTarget := trctracehttp.NewTarget("local", Collector())
	otherTargets := make([]*trctracehttp.Target, 0, len(groups))
	for groupName, urls := range groups {
		var groupSearcher trctrace.MultiSearcher
		for _, url := range urls {
			instanceSearcher := trctracehttp.NewClient(http.DefaultClient, url)
			groupSearcher = append(groupSearcher, instanceSearcher)
		}
		groupTarget := trctracehttp.NewTarget(groupName, groupSearcher)
		otherTargets = append(otherTargets, groupTarget)
	}
	server, err := trctracehttp.NewServer(trctracehttp.ServerConfig{
		Local: localTarget,
		Other: otherTargets,
	})
	if err != nil {
		panic(err)
	}
	return server
}

type ComplexConfig struct {
	LocalName string
	Instances map[string]string
	Groups    map[string][]string
}

func ComplexHandler(cfg ComplexConfig) http.Handler {
	localTarget := trctracehttp.NewTarget(cfg.LocalName, Collector())
	otherTargets := make([]*trctracehttp.Target, 0, len(cfg.Groups))
	for groupName, instanceNames := range cfg.Groups {
		var groupSearcher trctrace.MultiSearcher
		for _, instanceName := range instanceNames {
			url, ok := cfg.Instances[instanceName]
			if !ok {
				panic(fmt.Errorf("group %s instance %s is not defined", groupName, instanceName))
			}
			instanceSearcher := trctracehttp.NewClient(http.DefaultClient, url)
			groupSearcher = append(groupSearcher, instanceSearcher)
		}
		groupTarget := trctracehttp.NewTarget(groupName, groupSearcher)
		otherTargets = append(otherTargets, groupTarget)
	}
	server, err := trctracehttp.NewServer(trctracehttp.ServerConfig{
		Local: localTarget,
		Other: otherTargets,
	})
	if err != nil {
		panic(err)
	}
	return server
}
