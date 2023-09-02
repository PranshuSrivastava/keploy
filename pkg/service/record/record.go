package record

import (
	"go.keploy.io/server/pkg/hooks"
	"go.keploy.io/server/pkg/models"
	"go.keploy.io/server/pkg/platform/yaml"
	"go.keploy.io/server/pkg/proxy"
	"go.uber.org/zap"
	"fmt"
)

var Emoji = "\U0001F430" + " Keploy:"

type recorder struct {
	logger *zap.Logger
	proxyInterface proxy.ProxyInterface
}

// func NewRecorderWithProxy(logger *zap.Logger, proxySet proxy.ProxyInterface) Recorder {
// 	return &recorder{
// 		logger: logger,

// 	}
// }
//Overload the NewRecorder function to accept only the logger
func NewRecorder(logger *zap.Logger, proxySet proxy.ProxyInterface) Recorder {
	return &recorder{
		logger: logger,
		proxyInterface: proxySet,
	}
}

// func (r *recorder) CaptureTraffic(tcsPath, mockPath string, appCmd, appContainer, appNetwork string, Delay uint64) {
func (r *recorder) CaptureTraffic(path string, appCmd, appContainer, appNetwork string, Delay uint64) {
	models.SetMode(models.MODE_RECORD)

	ys := yaml.NewYamlStore(r.logger)
	// start the proxies
	// ps := proxy.BootProxies(r.logger, proxy.Option{}, appCmd)
	dirName, err := ys.NewSessionIndex(path)
	if err != nil {
		r.logger.Error("failed to find the directroy name for the session", zap.Error(err))
		return
	}
	path += "/" + dirName
	// Initiate the hooks and update the vaccant ProxyPorts map
	loadedHooks := hooks.NewHook(path, ys, r.logger)
	if err := loadedHooks.LoadHooks(appCmd, appContainer); err != nil {
		return
	}

	var test proxy.ProxySetInterface = (*proxy.ProxySet)(nil)
	fmt.Println(test)
	// start the proxies

	ps := r.proxyInterface.BootProxies(r.logger, proxy.Option{}, appCmd, appContainer)

	//proxy fetches the destIp and destPort from the redirect proxy map
	ps.SetHook(loadedHooks)

	//Sending Proxy Ip & Port to the ebpf program
	if err := loadedHooks.SendProxyInfo(ps.GetIP4(), ps.GetPort(), ps.GetIP6()); err != nil {
		return
	}
	// time.

	// start user application
	if err := loadedHooks.LaunchUserApplication(appCmd, appContainer, appNetwork, Delay); err != nil {
		r.logger.Error("failed to process user application hence stopping keploy", zap.Error(err))
		loadedHooks.Stop(true)
		ps.StopProxyServer()
		return
	}

	// Enable Pid Filtering
	// loadedHooks.EnablePidFilter()
	// ps.FilterPid = true

	// stop listening for the eBPF events
	loadedHooks.Stop(false)

	//stop listening for proxy server
	ps.StopProxyServer()
}
