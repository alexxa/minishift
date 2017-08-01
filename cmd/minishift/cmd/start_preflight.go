/*
Copyright (C) 2017 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/docker/machine/libmachine/drivers"
	startFlags "github.com/minishift/minishift/cmd/minishift/cmd/config"
	miniutil "github.com/minishift/minishift/pkg/minishift/util"

	"github.com/minishift/minishift/pkg/util/os/atexit"
	"github.com/spf13/viper"
)

const (
	StorageDisk = "/mnt/sda1"
)

func preflightChecksBeforeStartingHost() {
	switch viper.GetString(startFlags.VmDriver.Name) {
	case "xhyve":
		preflightCheckSucceedsOrFails(
			startFlags.SkipCheckXHyveDriver.Name,
			checkXhyveDriver,
			"Checking if xhyve driver is installed",
			false, startFlags.WarnCheckXHyveDriver.Name,
			"See the 'Setting Up the Driver Plug-in' topic for more information")
	case "kvm":
		preflightCheckSucceedsOrFails(
			startFlags.SkipCheckKVMDriver.Name,
			checkKvmDriver,
			"Checking if KVM driver is installed",
			false, startFlags.WarnCheckXHyveDriver.Name,
			"See the 'Setting Up the Driver Plug-in' topic for more information")
	case "hyperv":
		preflightCheckSucceedsOrFails(
			startFlags.SkipCheckHyperVDriver.Name,
			checkHypervDriver,
			"Checking if Hyper-V driver is configured",
			false, startFlags.WarnCheckHyperVDriver.Name,
			"Hyper-V virtual switch is not set")
	}
}

func preflightChecksAfterStartingHost(driver drivers.Driver) {
	preflightCheckSucceedsOrFailsWithDriver(
		startFlags.SkipInstanceIP.Name,
		checkInstanceIP, driver,
		"Checking for IP address",
		false, startFlags.WarnInstanceIP.Name,
		"Error determining IP address")
	/*
		// This happens too late in the preflight, as provisioning needs an IP already
			preflightCheckSucceedsOrFailsWithDriver(
				startFlags.SkipCheckNetworkHost.Name,
				checkVMConnectivity, driver,
				"Checking if VM is reachable from host",
				startFlags.WarnCheckNetworkHost.Name,
				"Please check our troubleshooting guide")
	*/
	preflightCheckSucceedsOrFailsWithDriver(
		startFlags.SkipCheckNetworkPing.Name,
		checkIPConnectivity, driver,
		"Checking if external host is reachable from the Minishift VM",
		true, startFlags.WarnCheckNetworkPing.Name,
		"VM is unable to ping external host")
	preflightCheckSucceedsOrFailsWithDriver(
		startFlags.SkipCheckNetworkHTTP.Name,
		checkHttpConnectivity, driver,
		"Checking HTTP connectivity from the VM",
		true, startFlags.WarnCheckNetworkHTTP.Name,
		"VM cannot connect to external URL with HTTP")
	preflightCheckSucceedsOrFailsWithDriver(
		startFlags.SkipCheckStorageMount.Name,
		checkStorageMounted, driver,
		"Checking if persisten storage volume is mounted",
		false, startFlags.WarnCheckStorageMount.Name,
		"Persistent volume storage is not mounted")
	preflightCheckSucceedsOrFailsWithDriver(
		startFlags.SkipCheckStorageUsage.Name,
		checkStorageUsage, driver,
		"Checking available disk space",
		false, startFlags.WarnCheckStorageUsage.Name,
		"Insufficient disk space on the persistent storage volume")
}

type preflightCheckFunc func() bool
type preflightCheckWithDriverFunc func(driver drivers.Driver) bool

// setting configNameOverrideIfSkipped to true will SKIP the check
// setting treatAsWarning and/or configNameOverrideIfWarning to true will WARN if check failed instead or FAIL
func preflightCheckSucceedsOrFails(configNameOverrideIfSkipped string, execute preflightCheckFunc, message string, treatAsWarning bool, configNameOverrideIfWarning string, errorMessage string) {
	fmt.Printf("-- %s ... ", message)

	isConfiguredToSkip := viper.GetBool(configNameOverrideIfSkipped)
	isConfiguredToWarn := viper.GetBool(configNameOverrideIfWarning)

	if isConfiguredToSkip {
		fmt.Println("SKIP")
		return
	}

	if execute() {
		fmt.Println("OK")
		return
	}

	fmt.Println("FAIL")
	errorMessage = fmt.Sprintf("   %s", errorMessage)
	if isConfiguredToWarn || treatAsWarning {
		fmt.Println(errorMessage)
	} else {
		atexit.ExitWithMessage(1, errorMessage)
	}
}

func preflightCheckSucceedsOrFailsWithDriver(configNameOverrideIfSkipped string, execute preflightCheckWithDriverFunc, driver drivers.Driver, message string, treatAsWarning bool, configNameOverrideIfWarning string, errorMessage string) {
	fmt.Printf("-- %s ... ", message)

	isConfiguredToSkip := viper.GetBool(configNameOverrideIfSkipped)
	isConfiguredToWarn := viper.GetBool(configNameOverrideIfWarning)

	if isConfiguredToSkip {
		fmt.Println("SKIP")
		return
	}

	if execute(driver) {
		fmt.Println("OK")
		return
	}

	fmt.Println("FAIL")
	errorMessage = fmt.Sprintf("   %s", errorMessage)
	if isConfiguredToWarn || treatAsWarning {
		fmt.Println(errorMessage)
	} else {
		atexit.ExitWithMessage(1, errorMessage)
	}
}

func checkXhyveDriver() bool {
	path, err := exec.LookPath("docker-machine-driver-xhyve")

	if err != nil {
		return false
	}

	fi, _ := os.Stat(path)
	// follow symlinks
	if fi.Mode()&os.ModeSymlink != 0 {
		path, err = os.Readlink(path)
		if err != nil {
			return false
		}
	}
	fmt.Println("\n   Driver is available at", path)

	fmt.Printf("   Checking for setuid bit ... ")
	if fi.Mode()&os.ModeSetuid == 0 {
		return false
	}

	return true
}

func checkKvmDriver() bool {
	path, err := exec.LookPath("docker-machine-driver-kvm")
	if err != nil {
		return false
	}
	fmt.Printf(fmt.Sprintf("\n   Driver is available at %s ... ", path))

	return true
}

func checkHypervDriver() bool {
	switchEnv := os.Getenv("HYPERV_VIRTUAL_SWITCH")
	if switchEnv == "" {
		return false
	}
	return true
}

// Check to make sure the instance has an IPv4 address.
// HyperV will issue IPv6 addresses on Internal virtual switch
// https://github.com/minishift/minishift/issues/418
func checkInstanceIP(driver drivers.Driver) bool {
	ip, err := driver.GetIP()
	if err == nil && net.ParseIP(ip).To4() != nil {
		return true
	}
	return false
}

//
func checkVMConnectivity(driver drivers.Driver) bool {
	// used to check if the host can reach the VM
	ip, _ := driver.GetIP()

	cmd := exec.Command("ping", "-n 1", ip)
	stdoutStderr, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	fmt.Printf("%s\n", stdoutStderr)
	return false
}

// Checks if the VM has connectivity to the outside network
func checkIPConnectivity(driver drivers.Driver) bool {
	ipToPing := viper.GetString(startFlags.CheckNetworkPingHost.Name)
	if ipToPing == "" {
		ipToPing = "8.8.8.8"
	}

	fmt.Printf("\n   Pinging %s ... ", ipToPing)
	return miniutil.IsIPReachable(driver, ipToPing, false)
}

// Allows to test outside connectivity and possible proxy support
func checkHttpConnectivity(driver drivers.Driver) bool {
	urlToRetrieve := viper.GetString(startFlags.CheckNetworkHttpHost.Name)
	if urlToRetrieve == "" {
		urlToRetrieve = "http://minishift.io/index.html"
	}

	fmt.Printf("\n   Retrieving %s ... ", urlToRetrieve)
	return miniutil.IsRetrievable(driver, urlToRetrieve, false)
}

func checkStorageMounted(driver drivers.Driver) bool {
	mounted, _ := isMounted(driver, StorageDisk)
	return mounted
}

func checkStorageUsage(driver drivers.Driver) bool {
	usedPercentage := getDiskUsage(driver, StorageDisk)
	fmt.Printf("%s ", usedPercentage)
	usedPercentage = strings.TrimRight(usedPercentage, "%")
	usage, err := strconv.ParseInt(usedPercentage, 10, 8)
	if err != nil {
		return false
	}

	if usage > 80 && usage < 98 {
		fmt.Printf("!!! ")
	}
	if usage < 98 {
		return true
	}
	return false
}

func getDiskUsage(driver drivers.Driver, mountpoint string) string {
	cmd := fmt.Sprintf(
		"df -h %s | awk 'FNR > 1 {print $5}'",
		mountpoint)

	out, err := drivers.RunSSHCommandFromDriver(driver, cmd)

	if err != nil {
		return "ERR"
	}

	return strings.Trim(out, "\n")
}

func isMounted(driver drivers.Driver, mountpoint string) (bool, error) {
	cmd := fmt.Sprintf(
		"if grep -qs %s /proc/mounts; then echo '1'; else echo '0'; fi",
		mountpoint)

	out, err := drivers.RunSSHCommandFromDriver(driver, cmd)

	if err != nil {
		return false, err
	}
	if strings.Trim(out, "\n") == "0" {
		return false, nil
	}

	return true, nil
}
