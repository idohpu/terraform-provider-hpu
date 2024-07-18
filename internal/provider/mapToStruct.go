package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	pxapi "github.com/Telmate/proxmox-api-go/proxmox"
	"github.com/google/uuid"
	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// Converting from schema.TypeSet to map of id and conf for each device,
// which will be sent to Proxmox API.
func DevicesSetToMap(devicesSet *schema.Set) (pxapi.QemuDevices, error) {

	var err error
	devicesMap := pxapi.QemuDevices{}

	for _, set := range devicesSet.List() {
		setMap, isMap := set.(map[string]interface{})
		if isMap {
			setID := setMap["id"].(int)
			if _, ok := devicesMap[setID]; !ok {
				devicesMap[setID] = setMap
			} else {
				return nil, fmt.Errorf("unable to process set, received a duplicate ID '%v' check your configuration file", setID)
			}
		}
	}
	return devicesMap, err
}

// Drops an element from each map in a []map[string]interface{}
// this allows a quick and easy way to remove things like "id" that is added by the proxmox api go library
// when we instead encode that id as the list index (and thus terraform would reject it in a d.Set(..) call
// WARNING mutates the list fed in!  make a copy if you need to keep the original
func DropElementsFromMap(elements []string, mapList []map[string]interface{}) ([]map[string]interface{}, error) {
	for _, mapItem := range mapList {
		for _, elem := range elements {
			delete(mapItem, elem)
		}
	}
	return mapList, nil
}

// Consumes an API return (pxapi.QemuDevices) and "flattens" it into a []map[string]interface{} as
// expected by the terraform interface for TypeList
func FlattenDevicesList(proxmoxDevices pxapi.QemuDevices) ([]map[string]interface{}, error) {
	flattenedDevices := make([]map[string]interface{}, 0, 1)

	numDevices := len(proxmoxDevices)
	if numDevices == 0 {
		return flattenedDevices, nil
	}

	// QemuDevices is a map[int]map[string]interface{}
	// we loop by index here to ensure that the devices remain in the same order
	for index := 0; index < numDevices; index++ {
		thisDevice := proxmoxDevices[index]
		thisFlattenedDevice := make(map[string]interface{})

		if thisDevice == nil {
			continue
		}

		for configuration, value := range thisDevice {
			thisFlattenedDevice[configuration] = value
		}

		flattenedDevices = append(flattenedDevices, thisFlattenedDevice)
	}

	return flattenedDevices, nil
}

func BuildSmbiosArgs(smbiosList []interface{}) string {
	useBase64 := false
	if len(smbiosList) == 0 {
		return ""
	}

	smbiosArgs := []string{}
	for _, v := range smbiosList {
		for conf, value := range v.(map[string]interface{}) {
			switch conf {

			case "uuid":
				var s string
				if value.(string) == "" {
					s = fmt.Sprintf("%s=%s", conf, uuid.New().String())
				} else {
					s = fmt.Sprintf("%s=%s", conf, value.(string))
				}
				smbiosArgs = append(smbiosArgs, s)

			case "serial", "manufacturer", "product", "version", "sku", "family":
				if value.(string) == "" {
					continue
				} else {
					s := fmt.Sprintf("%s=%s", conf, base64.StdEncoding.EncodeToString([]byte(value.(string))))
					smbiosArgs = append(smbiosArgs, s)
					useBase64 = true
				}
			default:
				continue
			}
		}
	}
	if useBase64 {
		smbiosArgs = append(smbiosArgs, "base64=1")
	}

	return strings.Join(smbiosArgs, ",")
}

func ReadSmbiosArgs(smbios string) []interface{} {
	if smbios == "" {
		return nil
	}

	smbiosArgs := []interface{}{}
	smbiosMap := make(map[string]interface{}, 0)
	for _, l := range strings.Split(smbios, ",") {
		if l == "" || l == "base64=1" {
			continue
		}
		parsedParameter, err := url.ParseQuery(l)
		if err != nil {
			log.Fatal(err)
		}
		for k, v := range parsedParameter {
			decodedString, err := base64.StdEncoding.DecodeString(v[0])
			if err != nil {
				decodedString = []byte(v[0])
			}
			smbiosMap[k] = string(decodedString)
		}
	}

	return append(smbiosArgs, smbiosMap)
}

// Consumes a terraform TypeList of a Qemu Device (network, hard drive, etc) and returns the "Expanded"
// version of the equivalent configuration that the API understands (the struct pxapi.QemuDevices).
// NOTE this expects the provided deviceList to be []map[string]interface{}.
func ExpandDevicesList(deviceList []interface{}) (pxapi.QemuDevices, error) {
	expandedDevices := make(pxapi.QemuDevices)

	if len(deviceList) == 0 {
		return expandedDevices, nil
	}

	for index, deviceInterface := range deviceList {
		thisDeviceMap := deviceInterface.(map[string]interface{})

		// allocate an expandedDevice, we'll append it to the list at the end of this loop
		thisExpandedDevice := make(map[string]interface{})

		// bail out if the device is empty, it is meaningless in this context
		if thisDeviceMap == nil {
			continue
		}

		// this is a map of string->interface, loop over it and move it into
		// the qemuDevices struct
		for configuration, value := range thisDeviceMap {
			thisExpandedDevice[configuration] = value
		}

		expandedDevices[index] = thisExpandedDevice
	}

	return expandedDevices, nil
}

// Update schema.TypeSet with new values comes from Proxmox API.
// TODO: remove these set functions and convert attributes using a set to a list instead.
func UpdateDevicesSet(
	devicesSet *schema.Set,
	devicesMap pxapi.QemuDevices,
	idKey string,
) *schema.Set {

	// configDevicesMap, _ := DevicesSetToMap(devicesSet)

	// activeDevicesMap := updateDevicesDefaults(devicesMap, configDevicesMap)
	activeDevicesMap := devicesMap

	for _, setConf := range devicesSet.List() {
		devicesSet.Remove(setConf)
		setConfMap := setConf.(map[string]interface{})
		deviceID := setConfMap[idKey].(int)
		setConfMap = adaptDeviceToConf(setConfMap, activeDevicesMap[deviceID])
		devicesSet.Add(setConfMap)
	}

	return devicesSet
}

func initConnInfo(ctx context.Context,
	d *schema.ResourceData,
	pconf *providerConfiguration,
	client *pxapi.Client,
	vmr *pxapi.VmRef,
	config *pxapi.ConfigQemu,
	lock *pmApiLockHolder,
) diag.Diagnostics {

	logger, _ := CreateSubLogger("initConnInfo")

	var err error
	var lasterr error
	var interfaces []pxapi.AgentNetworkInterface
	var diags diag.Diagnostics
	// allow user to opt-out of setting the connection info for the resource
	if !d.Get("define_connection_info").(bool) {
		log.Printf("[INFO][initConnInfo] define_connection_info is %t, no further action", d.Get("define_connection_info").(bool))
		logger.Info().Int("vmid", vmr.VmId()).Msgf("define_connection_info is %t, no further action", d.Get("define_connection_info").(bool))

		diags = append(diags, diag.Diagnostic{
			Severity:      diag.Warning,
			Summary:       "define_connection_info is %t, no further action.",
			Detail:        "define_connection_info is %t, no further action",
			AttributePath: cty.Path{},
		})
		return diags
	}
	// allow user to opt-out of setting the connection info for the resource
	if d.Get("agent") != 1 {
		log.Printf("[INFO][initConnInfo] qemu agent is disabled from proxmox config, cant communicate with vm.")
		logger.Info().Int("vmid", vmr.VmId()).Msgf("qemu agent is disabled from proxmox config, cant communicate with vm.")
		diags = append(diags, diag.Diagnostic{
			Severity:      diag.Warning,
			Summary:       "Qemu Guest Agent support is disabled from proxmox config.",
			Detail:        "Qemu Guest Agent support is required to make communications with the VM",
			AttributePath: cty.Path{},
		})
		return diags
	}

	log.Print("[INFO][initConnInfo] trying to get vm ip address for provisioner")
	logger.Info().Int("vmid", vmr.VmId()).Msgf("trying to get vm ip address for provisioner")
	sshPort := "22"
	sshHost := ""
	// assume guest agent not running yet or not enabled
	guestAgentRunning := false

	// wait until the os has started the guest agent
	guestAgentTimeout := d.Timeout(schema.TimeoutCreate)
	guestAgentWaitEnd := time.Now().Add(time.Duration(guestAgentTimeout))
	log.Printf("[DEBUG][initConnInfo] retrying for at most  %v minutes before giving up", guestAgentTimeout)
	log.Printf("[DEBUG][initConnInfo] retries will end at %s", guestAgentWaitEnd)
	logger.Debug().Int("vmid", vmr.VmId()).Msgf("retrying for at most  %v minutes before giving up", guestAgentTimeout)
	logger.Debug().Int("vmid", vmr.VmId()).Msgf("retries will end at %s", guestAgentWaitEnd)

	for time.Now().Before(guestAgentWaitEnd) {
		interfaces, err = client.GetVmAgentNetworkInterfaces(vmr)
		lasterr = err
		if err != nil {
			log.Printf("[DEBUG][initConnInfo] check ip result error %s", err.Error())
			logger.Debug().Int("vmid", vmr.VmId()).Msgf("check ip result error %s", err.Error())
		} else if err == nil {
			lasterr = nil
			log.Print("[INFO][initConnInfo] found working QEMU Agent")
			log.Printf("[DEBUG][initConnInfo] interfaces found: %v", interfaces)
			logger.Info().Int("vmid", vmr.VmId()).Msgf("found working QEMU Agent")
			logger.Debug().Int("vmid", vmr.VmId()).Msgf("interfaces found: %v", interfaces)

			guestAgentRunning = true
			break
		} else if !strings.Contains(err.Error(), "500 QEMU guest agent is not running") {
			// "not running" means either not installed or not started yet.
			// any other error should not happen here
			return diag.FromErr(err)
		}
		// wait before next try
		time.Sleep(time.Duration(d.Get("additional_wait").(int)) * time.Second)
	}
	if lasterr != nil {
		log.Printf("[INFO][initConnInfo] error from PVE: \"%s\"\n, QEMU Agent is enabled in you configuration but non installed/not working on your vm", lasterr)
		logger.Info().Int("vmid", vmr.VmId()).Msgf("error from PVE: \"%s\"\n, QEMU Agent is enabled in you configuration but non installed/not working on your vm", lasterr)
		return diag.FromErr(fmt.Errorf("error from PVE: \"%s\"\n, QEMU Agent is enabled in you configuration but non installed/not working on your vm", lasterr))
	}
	vmConfig, err := client.GetVmConfig(vmr)
	if err != nil {
		return diag.FromErr(err)
	}
	log.Print("[INFO][initConnInfo] trying to find IP address of first network card")
	logger.Info().Int("vmid", vmr.VmId()).Msgf("trying to find IP address of first network card")

	// wait until we find a valid ipv4 address
	log.Printf("[DEBUG][initConnInfo] checking network card...")
	logger.Debug().Int("vmid", vmr.VmId()).Msgf("checking network card...")
	for guestAgentRunning && time.Now().Before(guestAgentWaitEnd) {
		log.Printf("[DEBUG][initConnInfo] checking network card...")
		interfaces, err = client.GetVmAgentNetworkInterfaces(vmr)
		net0MacAddress := macAddressRegex.FindString(vmConfig["net0"].(string))
		if err != nil {
			log.Printf("[DEBUG][initConnInfo] checking network card error %s", err.Error())
			logger.Debug().Int("vmid", vmr.VmId()).Msgf("checking network card error %s", err.Error())
			// return err
		} else {
			log.Printf("[DEBUG][initConnInfo] checking network card loop")
			logger.Debug().Int("vmid", vmr.VmId()).Msgf("checking network card loop")
			for _, iface := range interfaces {
				if strings.EqualFold(strings.ToUpper(iface.MACAddress), strings.ToUpper(net0MacAddress)) {
					for _, addr := range iface.IPAddresses {
						if addr.IsGlobalUnicast() && strings.Count(addr.String(), ":") < 2 {
							log.Printf("[DEBUG][initConnInfo] Found IP address: %s", addr.String())
							logger.Debug().Int("vmid", vmr.VmId()).Msgf("Found IP address: %s", addr.String())
							sshHost = addr.String()
						}
					}
				}
			}
			if sshHost != "" {
				log.Printf("[DEBUG][initConnInfo] sshHost not empty: %s", sshHost)
				logger.Debug().Int("vmid", vmr.VmId()).Msgf("sshHost not empty: %s", sshHost)
				break
			}
		}
		// wait before next try
		time.Sleep(time.Duration(d.Get("additional_wait").(int)) * time.Second)
	}
	// todo - log a warning if we couldn't get an IP

	if config.HasCloudInit() {
		log.Print("[DEBUG][initConnInfo] vm has a cloud-init configuration")
		logger.Debug().Int("vmid", vmr.VmId()).Msgf(" vm has a cloud-init configuration")
		_, ipconfig0Set := d.GetOk("ipconfig0")
		if ipconfig0Set {
			vmState, err := client.GetVmState(vmr)
			log.Printf("[DEBUG][initConnInfo] cloudinitcheck vm state %v", vmState)
			logger.Debug().Int("vmid", vmr.VmId()).Msgf("cloudinitcheck vm state %v", vmState)
			if err != nil {
				log.Printf("[DEBUG][initConnInfo] vmstate error %s", err.Error())
				logger.Debug().Int("vmid", vmr.VmId()).Msgf("vmstate error %s", err.Error())
				return diag.FromErr(err)
			}
			if d.Get("ipconfig0").(string) != "ip=dhcp" || vmState["agent"] == nil || vmState["agent"].(float64) != 1 {
				// parse IP address out of ipconfig0
				ipMatch := rxIPconfig.FindStringSubmatch(d.Get("ipconfig0").(string))
				if sshHost == "" {
					sshHost = ipMatch[1]
				}
				ipconfig0 := net.ParseIP(strings.Split(ipMatch[1], ":")[0])
				interfaces, err = client.GetVmAgentNetworkInterfaces(vmr)
				log.Printf("[DEBUG][initConnInfo] ipconfig0 interfaces: %v", interfaces)
				logger.Debug().Int("vmid", vmr.VmId()).Msgf("ipconfig0 interfaces %v", interfaces)
				if err != nil {
					return diag.FromErr(err)
				} else {
					for _, iface := range interfaces {
						if sshHost == ipMatch[1] {
							break
						}
						for _, addr := range iface.IPAddresses {
							if addr.Equal(ipconfig0) {
								sshHost = ipMatch[1]
								break
							}
						}
					}
				}
			}
		}

		log.Print("[DEBUG][initConnInfo] found an ip configuration")
		logger.Debug().Int("vmid", vmr.VmId()).Msgf("Found an ip configuration")
		// Check if we got a specified port
		if strings.Contains(sshHost, ":") {
			sshParts := strings.Split(sshHost, ":")
			sshHost = sshParts[0]
			sshPort = sshParts[1]
		}
	}
	if sshHost == "" {
		log.Print("[DEBUG][initConnInfo] Cannot find any IP address")
		logger.Debug().Int("vmid", vmr.VmId()).Msgf("Cannot find any IP address")
		return diag.FromErr(fmt.Errorf("cannot find any IP address"))
	}

	log.Printf("[DEBUG][initConnInfo] this is the vm configuration: %s %s", sshHost, sshPort)
	logger.Debug().Int("vmid", vmr.VmId()).Msgf("this is the vm configuration: %s %s", sshHost, sshPort)

	// Optional convenience attributes for provisioners
	err = d.Set("default_ipv4_address", sshHost)
	diags = append(diags, diag.FromErr(err)...)
	err = d.Set("ssh_host", sshHost)
	diags = append(diags, diag.FromErr(err)...)
	err = d.Set("ssh_port", sshPort)
	diags = append(diags, diag.FromErr(err)...)

	// This connection INFO is longer shared up to the providers :-(
	d.SetConnInfo(map[string]string{
		"type": "ssh",
		"host": sshHost,
		"port": sshPort,
	})
	return diags
}

func setCloudInitDisk(d *schema.ResourceData, config *pxapi.ConfigQemu) {
	storage := d.Get("cloudinit_cdrom_storage").(string)
	if storage != "" {
		config.Disks.Ide.Disk_3 = &pxapi.QemuIdeStorage{CloudInit: &pxapi.QemuCloudInitDisk{
			Format:  pxapi.QemuDiskFormat_Raw,
			Storage: storage,
		}}
	}
}

func getCloudInitDisk(config *pxapi.QemuStorages) string {
	if config != nil && config.Ide != nil && config.Ide.Disk_3 != nil && config.Ide.Disk_3.CloudInit != nil {
		return config.Ide.Disk_3.CloudInit.Storage
	}
	return ""
}

// Map struct to the terraform schema
func mapFromStruct_ConfigQemu(config *pxapi.QemuStorages) []interface{} {
	if config == nil {
		return nil
	}
	ide := mapFromStruct_QemuIdeDisks(config.Ide)
	sata := mapFromStruct_QemuSataDisks(config.Sata)
	scsi := mapFromStruct_QemuScsiDisks(config.Scsi)
	virtio := mapFromStruct_QemuVirtIODisks(config.VirtIO)
	if ide == nil && sata == nil && scsi == nil && virtio == nil {
		return nil
	}
	return []interface{}{
		map[string]interface{}{
			"ide":    ide,
			"sata":   sata,
			"scsi":   scsi,
			"virtio": virtio,
		},
	}
}

func mapFormStruct_IsoFile(config *pxapi.IsoFile) string {
	if config == nil {
		return ""
	}
	return config.Storage + ":iso/" + config.File
}

func mapFromStruct_LinkedCloneId(id *uint) int {
	if id != nil {
		return int(*id)
	}
	return -1
}

func mapFormStruct_QemuCdRom(config *pxapi.QemuCdRom) []interface{} {
	if config == nil {
		return nil
	}
	return []interface{}{
		map[string]interface{}{
			"cdrom": []interface{}{
				map[string]interface{}{
					"iso":         mapFormStruct_IsoFile(config.Iso),
					"passthrough": config.Passthrough,
				},
			},
		},
	}
}

func mapFormStruct_QemuDiskBandwidth(params map[string]interface{}, config pxapi.QemuDiskBandwidth) {
	params["mbps_r_burst"] = float64(config.MBps.ReadLimit.Burst)
	params["mbps_r_concurrent"] = float64(config.MBps.ReadLimit.Concurrent)
	params["mbps_wr_burst"] = float64(config.MBps.WriteLimit.Burst)
	params["mbps_wr_concurrent"] = float64(config.MBps.ReadLimit.Burst)
	params["iops_r_burst"] = int(config.Iops.ReadLimit.Burst)
	params["iops_r_burst_length"] = int(config.Iops.ReadLimit.BurstDuration)
	params["iops_r_concurrent"] = int(config.Iops.ReadLimit.Concurrent)
	params["iops_wr_burst"] = int(config.Iops.WriteLimit.Burst)
	params["iops_wr_burst_length"] = int(config.Iops.WriteLimit.BurstDuration)
	params["iops_wr_concurrent"] = int(config.Iops.WriteLimit.Concurrent)
}

func mapFromStruct_QemuGuestAgent(d *schema.ResourceData, config *pxapi.QemuGuestAgent) {
	if config == nil {
		return
	}
	if config.Enable != nil {
		if *config.Enable {
			d.Set("agent", 1)
		} else {
			d.Set("agent", 0)
		}
	}
}

func mapFromStruct_QemuIdeDisks(config *pxapi.QemuIdeDisks) []interface{} {
	if config == nil {
		return nil
	}
	ide_0 := mapFromStruct_QemuIdeStorage(config.Disk_0, "ide0")
	ide_1 := mapFromStruct_QemuIdeStorage(config.Disk_1, "ide1")
	ide_2 := mapFromStruct_QemuIdeStorage(config.Disk_2, "ide2")
	if ide_0 == nil && ide_1 == nil && ide_2 == nil {
		return nil
	}
	return []interface{}{
		map[string]interface{}{
			"ide0": ide_0,
			"ide1": ide_1,
			"ide2": ide_2,
		},
	}
}

func mapFromStruct_QemuIdeStorage(config *pxapi.QemuIdeStorage, setting string) []interface{} {
	if config == nil {
		return nil
	}
	if config.Disk != nil {
		mapParams := map[string]interface{}{
			"asyncio":        string(config.Disk.AsyncIO),
			"backup":         config.Disk.Backup,
			"cache":          string(config.Disk.Cache),
			"discard":        config.Disk.Discard,
			"emulatessd":     config.Disk.EmulateSSD,
			"format":         string(config.Disk.Format),
			"id":             int(config.Disk.Id),
			"linked_disk_id": mapFromStruct_LinkedCloneId(config.Disk.LinkedDiskId),
			"replicate":      config.Disk.Replicate,
			"serial":         string(config.Disk.Serial),
			"size":           convert_KibibytesToString(int64(config.Disk.SizeInKibibytes)),
			"storage":        string(config.Disk.Storage),
		}
		mapFormStruct_QemuDiskBandwidth(mapParams, config.Disk.Bandwidth)
		return []interface{}{
			map[string]interface{}{
				"disk": []interface{}{mapParams},
			},
		}
	}
	if config.Passthrough != nil {
		mapParams := map[string]interface{}{
			"asyncio":    string(config.Passthrough.AsyncIO),
			"backup":     config.Passthrough.Backup,
			"cache":      string(config.Passthrough.Cache),
			"discard":    config.Passthrough.Discard,
			"emulatessd": config.Passthrough.EmulateSSD,
			"file":       config.Passthrough.File,
			"replicate":  config.Passthrough.Replicate,
			"serial":     string(config.Passthrough.Serial),
			"size":       convert_KibibytesToString(int64(config.Passthrough.SizeInKibibytes)),
		}
		mapFormStruct_QemuDiskBandwidth(mapParams, config.Passthrough.Bandwidth)
		return []interface{}{
			map[string]interface{}{
				"passthrough": []interface{}{mapParams},
			},
		}
	}
	return mapFormStruct_QemuCdRom(config.CdRom)
}

func mapFromStruct_QemuSataDisks(config *pxapi.QemuSataDisks) []interface{} {
	if config == nil {
		return nil
	}
	return []interface{}{
		map[string]interface{}{
			"sata0": mapFromStruct_QemuSataStorage(config.Disk_0, "sata0"),
			"sata1": mapFromStruct_QemuSataStorage(config.Disk_1, "sata1"),
			"sata2": mapFromStruct_QemuSataStorage(config.Disk_2, "sata2"),
			"sata3": mapFromStruct_QemuSataStorage(config.Disk_3, "sata3"),
			"sata4": mapFromStruct_QemuSataStorage(config.Disk_4, "sata4"),
			"sata5": mapFromStruct_QemuSataStorage(config.Disk_5, "sata5"),
		},
	}
}

func mapFromStruct_QemuSataStorage(config *pxapi.QemuSataStorage, setting string) []interface{} {
	if config == nil {
		return nil
	}
	if config.Disk != nil {
		mapParams := map[string]interface{}{
			"asyncio":        string(config.Disk.AsyncIO),
			"backup":         config.Disk.Backup,
			"cache":          string(config.Disk.Cache),
			"discard":        config.Disk.Discard,
			"emulatessd":     config.Disk.EmulateSSD,
			"format":         string(config.Disk.Format),
			"id":             int(config.Disk.Id),
			"linked_disk_id": mapFromStruct_LinkedCloneId(config.Disk.LinkedDiskId),
			"replicate":      config.Disk.Replicate,
			"serial":         string(config.Disk.Serial),
			"size":           convert_KibibytesToString(int64(config.Disk.SizeInKibibytes)),
			"storage":        string(config.Disk.Storage),
		}
		mapFormStruct_QemuDiskBandwidth(mapParams, config.Disk.Bandwidth)
		return []interface{}{
			map[string]interface{}{
				"disk": []interface{}{mapParams},
			},
		}
	}
	if config.Passthrough != nil {
		mapParams := map[string]interface{}{
			"asyncio":    string(config.Passthrough.AsyncIO),
			"backup":     config.Passthrough.Backup,
			"cache":      string(config.Passthrough.Cache),
			"discard":    config.Passthrough.Discard,
			"emulatessd": config.Passthrough.EmulateSSD,
			"file":       config.Passthrough.File,
			"replicate":  config.Passthrough.Replicate,
			"serial":     string(config.Passthrough.Serial),
			"size":       convert_KibibytesToString(int64(config.Disk.SizeInKibibytes)),
		}
		mapFormStruct_QemuDiskBandwidth(mapParams, config.Passthrough.Bandwidth)
		return []interface{}{
			map[string]interface{}{
				"passthrough": []interface{}{mapParams},
			},
		}
	}
	return mapFormStruct_QemuCdRom(config.CdRom)
}

func mapFromStruct_QemuScsiDisks(config *pxapi.QemuScsiDisks) []interface{} {
	if config == nil {
		return nil
	}
	return []interface{}{
		map[string]interface{}{
			"scsi0":  mapFromStruct_QemuScsiStorage(config.Disk_0, "scsi0"),
			"scsi1":  mapFromStruct_QemuScsiStorage(config.Disk_1, "scsi1"),
			"scsi2":  mapFromStruct_QemuScsiStorage(config.Disk_2, "scsi2"),
			"scsi3":  mapFromStruct_QemuScsiStorage(config.Disk_3, "scsi3"),
			"scsi4":  mapFromStruct_QemuScsiStorage(config.Disk_4, "scsi4"),
			"scsi5":  mapFromStruct_QemuScsiStorage(config.Disk_5, "scsi5"),
			"scsi6":  mapFromStruct_QemuScsiStorage(config.Disk_6, "scsi6"),
			"scsi7":  mapFromStruct_QemuScsiStorage(config.Disk_7, "scsi7"),
			"scsi8":  mapFromStruct_QemuScsiStorage(config.Disk_8, "scsi8"),
			"scsi9":  mapFromStruct_QemuScsiStorage(config.Disk_9, "scsi9"),
			"scsi10": mapFromStruct_QemuScsiStorage(config.Disk_10, "scsi10"),
			"scsi11": mapFromStruct_QemuScsiStorage(config.Disk_11, "scsi11"),
			"scsi12": mapFromStruct_QemuScsiStorage(config.Disk_12, "scsi12"),
			"scsi13": mapFromStruct_QemuScsiStorage(config.Disk_13, "scsi13"),
			"scsi14": mapFromStruct_QemuScsiStorage(config.Disk_14, "scsi14"),
			"scsi15": mapFromStruct_QemuScsiStorage(config.Disk_15, "scsi15"),
			"scsi16": mapFromStruct_QemuScsiStorage(config.Disk_16, "scsi16"),
			"scsi17": mapFromStruct_QemuScsiStorage(config.Disk_17, "scsi17"),
			"scsi18": mapFromStruct_QemuScsiStorage(config.Disk_18, "scsi18"),
			"scsi19": mapFromStruct_QemuScsiStorage(config.Disk_19, "scsi19"),
			"scsi20": mapFromStruct_QemuScsiStorage(config.Disk_20, "scsi20"),
			"scsi21": mapFromStruct_QemuScsiStorage(config.Disk_21, "scsi21"),
			"scsi22": mapFromStruct_QemuScsiStorage(config.Disk_22, "scsi22"),
			"scsi23": mapFromStruct_QemuScsiStorage(config.Disk_23, "scsi23"),
			"scsi24": mapFromStruct_QemuScsiStorage(config.Disk_24, "scsi24"),
			"scsi25": mapFromStruct_QemuScsiStorage(config.Disk_25, "scsi25"),
			"scsi26": mapFromStruct_QemuScsiStorage(config.Disk_26, "scsi26"),
			"scsi27": mapFromStruct_QemuScsiStorage(config.Disk_27, "scsi27"),
			"scsi28": mapFromStruct_QemuScsiStorage(config.Disk_28, "scsi28"),
			"scsi29": mapFromStruct_QemuScsiStorage(config.Disk_29, "scsi29"),
			"scsi30": mapFromStruct_QemuScsiStorage(config.Disk_30, "scsi30"),
		},
	}
}

func mapFromStruct_QemuScsiStorage(config *pxapi.QemuScsiStorage, setting string) []interface{} {
	if config == nil {
		return nil
	}
	if config.Disk != nil {
		mapParams := map[string]interface{}{
			"asyncio":        string(config.Disk.AsyncIO),
			"backup":         config.Disk.Backup,
			"cache":          string(config.Disk.Cache),
			"discard":        config.Disk.Discard,
			"emulatessd":     config.Disk.EmulateSSD,
			"format":         string(config.Disk.Format),
			"id":             int(config.Disk.Id),
			"iothread":       config.Disk.IOThread,
			"linked_disk_id": mapFromStruct_LinkedCloneId(config.Disk.LinkedDiskId),
			"readonly":       config.Disk.ReadOnly,
			"replicate":      config.Disk.Replicate,
			"serial":         string(config.Disk.Serial),
			"size":           convert_KibibytesToString(int64(config.Disk.SizeInKibibytes)),
			"storage":        string(config.Disk.Storage),
		}
		mapFormStruct_QemuDiskBandwidth(mapParams, config.Disk.Bandwidth)
		return []interface{}{
			map[string]interface{}{
				"disk": []interface{}{mapParams},
			},
		}
	}
	if config.Passthrough != nil {
		mapParams := map[string]interface{}{
			"asyncio":    string(config.Passthrough.AsyncIO),
			"backup":     config.Passthrough.Backup,
			"cache":      string(config.Passthrough.Cache),
			"discard":    config.Passthrough.Discard,
			"emulatessd": config.Passthrough.EmulateSSD,
			"file":       config.Passthrough.File,
			"iothread":   config.Passthrough.IOThread,
			"readonly":   config.Passthrough.ReadOnly,
			"replicate":  config.Passthrough.Replicate,
			"serial":     string(config.Passthrough.Serial),
			"size":       convert_KibibytesToString(int64(config.Passthrough.SizeInKibibytes)),
		}
		mapFormStruct_QemuDiskBandwidth(mapParams, config.Passthrough.Bandwidth)
		return []interface{}{
			map[string]interface{}{
				"passthrough": []interface{}{mapParams},
			},
		}
	}
	return mapFormStruct_QemuCdRom(config.CdRom)
}

func mapFromStruct_QemuVirtIODisks(config *pxapi.QemuVirtIODisks) []interface{} {
	if config == nil {
		return nil
	}
	return []interface{}{
		map[string]interface{}{
			"virtio0":  mapFromStruct_QemuVirtIOStorage(config.Disk_0, "virtio0"),
			"virtio1":  mapFromStruct_QemuVirtIOStorage(config.Disk_1, "virtio1"),
			"virtio2":  mapFromStruct_QemuVirtIOStorage(config.Disk_2, "virtio2"),
			"virtio3":  mapFromStruct_QemuVirtIOStorage(config.Disk_3, "virtio3"),
			"virtio4":  mapFromStruct_QemuVirtIOStorage(config.Disk_4, "virtio4"),
			"virtio5":  mapFromStruct_QemuVirtIOStorage(config.Disk_5, "virtio5"),
			"virtio6":  mapFromStruct_QemuVirtIOStorage(config.Disk_6, "virtio6"),
			"virtio7":  mapFromStruct_QemuVirtIOStorage(config.Disk_7, "virtio7"),
			"virtio8":  mapFromStruct_QemuVirtIOStorage(config.Disk_8, "virtio8"),
			"virtio9":  mapFromStruct_QemuVirtIOStorage(config.Disk_9, "virtio9"),
			"virtio10": mapFromStruct_QemuVirtIOStorage(config.Disk_10, "virtio10"),
			"virtio11": mapFromStruct_QemuVirtIOStorage(config.Disk_11, "virtio11"),
			"virtio12": mapFromStruct_QemuVirtIOStorage(config.Disk_12, "virtio12"),
			"virtio13": mapFromStruct_QemuVirtIOStorage(config.Disk_13, "virtio13"),
			"virtio14": mapFromStruct_QemuVirtIOStorage(config.Disk_14, "virtio14"),
			"virtio15": mapFromStruct_QemuVirtIOStorage(config.Disk_15, "virtio15"),
		},
	}
}

func mapFromStruct_QemuVirtIOStorage(config *pxapi.QemuVirtIOStorage, setting string) []interface{} {
	if config == nil {
		return nil
	}
	mapFormStruct_QemuCdRom(config.CdRom)
	if config.Disk != nil {
		mapParams := map[string]interface{}{
			"asyncio":        string(config.Disk.AsyncIO),
			"backup":         config.Disk.Backup,
			"cache":          string(config.Disk.Cache),
			"discard":        config.Disk.Discard,
			"format":         string(config.Disk.Format),
			"id":             int(config.Disk.Id),
			"iothread":       config.Disk.IOThread,
			"linked_disk_id": mapFromStruct_LinkedCloneId(config.Disk.LinkedDiskId),
			"readonly":       config.Disk.ReadOnly,
			"replicate":      config.Disk.Replicate,
			"serial":         string(config.Disk.Serial),
			"size":           convert_KibibytesToString(int64(config.Disk.SizeInKibibytes)),
			"storage":        string(config.Disk.Storage),
		}
		mapFormStruct_QemuDiskBandwidth(mapParams, config.Disk.Bandwidth)
		return []interface{}{
			map[string]interface{}{
				"disk": []interface{}{mapParams},
			},
		}
	}
	if config.Passthrough != nil {
		mapParams := map[string]interface{}{
			"asyncio":   string(config.Passthrough.AsyncIO),
			"backup":    config.Passthrough.Backup,
			"cache":     string(config.Passthrough.Cache),
			"discard":   config.Passthrough.Discard,
			"file":      config.Passthrough.File,
			"iothread":  config.Passthrough.IOThread,
			"readonly":  config.Passthrough.ReadOnly,
			"replicate": config.Passthrough.Replicate,
			"serial":    string(config.Passthrough.Serial),
			"size":      convert_KibibytesToString(int64(config.Passthrough.SizeInKibibytes)),
		}
		mapFormStruct_QemuDiskBandwidth(mapParams, config.Passthrough.Bandwidth)
		return []interface{}{
			map[string]interface{}{
				"passthrough": []interface{}{mapParams},
			},
		}
	}
	return mapFormStruct_QemuCdRom(config.CdRom)
}

// Map the terraform schema to sdk struct
func mapToStruct_IsoFile(iso string) *pxapi.IsoFile {
	if iso == "" {
		return nil
	}
	storage, fileWithPrefix, cut := strings.Cut(iso, ":")
	if !cut {
		return nil
	}
	_, file, cut := strings.Cut(fileWithPrefix, "/")
	if !cut {
		return nil
	}
	return &pxapi.IsoFile{File: file, Storage: storage}
}

func mapToStruct_QemuCdRom(schema map[string]interface{}) (cdRom *pxapi.QemuCdRom) {
	schemaItem, ok := schema["cdrom"].([]interface{})
	if !ok {
		return
	}
	if len(schemaItem) != 1 || schemaItem[0] == nil {
		return &pxapi.QemuCdRom{}
	}
	cdRomSchema := schemaItem[0].(map[string]interface{})
	return &pxapi.QemuCdRom{
		Iso:         mapToStruct_IsoFile(cdRomSchema["iso"].(string)),
		Passthrough: cdRomSchema["passthrough"].(bool),
	}
}

func mapToStruct_QemuDiskBandwidth(schema map[string]interface{}) pxapi.QemuDiskBandwidth {
	return pxapi.QemuDiskBandwidth{
		MBps: pxapi.QemuDiskBandwidthMBps{
			ReadLimit: pxapi.QemuDiskBandwidthMBpsLimit{
				Burst:      pxapi.QemuDiskBandwidthMBpsLimitBurst(schema["mbps_r_burst"].(float64)),
				Concurrent: pxapi.QemuDiskBandwidthMBpsLimitConcurrent(schema["mbps_r_concurrent"].(float64)),
			},
			WriteLimit: pxapi.QemuDiskBandwidthMBpsLimit{
				Burst:      pxapi.QemuDiskBandwidthMBpsLimitBurst(schema["mbps_wr_burst"].(float64)),
				Concurrent: pxapi.QemuDiskBandwidthMBpsLimitConcurrent(schema["mbps_wr_concurrent"].(float64)),
			},
		},
		Iops: pxapi.QemuDiskBandwidthIops{
			ReadLimit: pxapi.QemuDiskBandwidthIopsLimit{
				Burst:         pxapi.QemuDiskBandwidthIopsLimitBurst(schema["iops_r_burst"].(int)),
				BurstDuration: uint(schema["iops_r_burst_length"].(int)),
				Concurrent:    pxapi.QemuDiskBandwidthIopsLimitConcurrent(schema["iops_r_concurrent"].(int)),
			},
			WriteLimit: pxapi.QemuDiskBandwidthIopsLimit{
				Burst:         pxapi.QemuDiskBandwidthIopsLimitBurst(schema["iops_wr_burst"].(int)),
				BurstDuration: uint(schema["iops_wr_burst_length"].(int)),
				Concurrent:    pxapi.QemuDiskBandwidthIopsLimitConcurrent(schema["iops_wr_concurrent"].(int)),
			},
		},
	}
}

func mapToStruct_QemuGuestAgent(d *schema.ResourceData) *pxapi.QemuGuestAgent {
	var tmpEnable bool
	if d.Get("agent").(int) == 1 {
		tmpEnable = true
	}
	return &pxapi.QemuGuestAgent{
		Enable: &tmpEnable,
	}
}

func mapToStruct_QemuIdeDisks(ide *pxapi.QemuIdeDisks, schema map[string]interface{}) {
	schemaItem, ok := schema["ide"].([]interface{})
	if !ok || len(schemaItem) != 1 || schemaItem[0] == nil {
		return
	}
	disks := schemaItem[0].(map[string]interface{})
	mapToStruct_QemuIdeStorage(ide.Disk_0, "ide0", disks)
	mapToStruct_QemuIdeStorage(ide.Disk_1, "ide1", disks)
	mapToStruct_QemuIdeStorage(ide.Disk_2, "ide2", disks)
}

func mapToStruct_QemuIdeStorage(ide *pxapi.QemuIdeStorage, key string, schema map[string]interface{}) {
	schemaItem, ok := schema[key].([]interface{})
	if !ok || len(schemaItem) != 1 || schemaItem[0] == nil {
		return
	}
	storageSchema := schemaItem[0].(map[string]interface{})
	tmpDisk, ok := storageSchema["disk"].([]interface{})
	if ok && len(tmpDisk) == 1 && tmpDisk[0] != nil {
		disk := tmpDisk[0].(map[string]interface{})
		ide.Disk = &pxapi.QemuIdeDisk{
			Backup:          disk["backup"].(bool),
			Bandwidth:       mapToStruct_QemuDiskBandwidth(disk),
			Discard:         disk["discard"].(bool),
			EmulateSSD:      disk["emulatessd"].(bool),
			Format:          pxapi.QemuDiskFormat(disk["format"].(string)),
			Replicate:       disk["replicate"].(bool),
			SizeInKibibytes: pxapi.QemuDiskSize(convert_SizeStringToKibibytes_Unsafe(disk["size"].(string))),
			Storage:         disk["storage"].(string),
		}
		if asyncIO, ok := disk["asyncio"].(string); ok {
			ide.Disk.AsyncIO = pxapi.QemuDiskAsyncIO(asyncIO)
		}
		if cache, ok := disk["cache"].(string); ok {
			ide.Disk.Cache = pxapi.QemuDiskCache(cache)
		}
		if serial, ok := disk["serial"].(string); ok {
			ide.Disk.Serial = pxapi.QemuDiskSerial(serial)
		}
		return
	}
	tmpPassthrough, ok := storageSchema["passthrough"].([]interface{})
	if ok && len(tmpPassthrough) == 1 && tmpPassthrough[0] != nil {
		passthrough := tmpPassthrough[0].(map[string]interface{})
		ide.Passthrough = &pxapi.QemuIdePassthrough{
			Backup:     passthrough["backup"].(bool),
			Bandwidth:  mapToStruct_QemuDiskBandwidth(passthrough),
			Discard:    passthrough["discard"].(bool),
			EmulateSSD: passthrough["emulatessd"].(bool),
			File:       passthrough["file"].(string),
			Replicate:  passthrough["replicate"].(bool),
		}
		if asyncIO, ok := passthrough["asyncio"].(string); ok {
			ide.Passthrough.AsyncIO = pxapi.QemuDiskAsyncIO(asyncIO)
		}
		if cache, ok := passthrough["cache"].(string); ok {
			ide.Passthrough.Cache = pxapi.QemuDiskCache(cache)
		}
		if serial, ok := passthrough["serial"].(string); ok {
			ide.Passthrough.Serial = pxapi.QemuDiskSerial(serial)
		}
		return
	}
	ide.CdRom = mapToStruct_QemuCdRom(storageSchema)
}

func mapToStruct_QemuSataDisks(sata *pxapi.QemuSataDisks, schema map[string]interface{}) {
	schemaItem, ok := schema["sata"].([]interface{})
	if !ok || len(schemaItem) != 1 || schemaItem[0] == nil {
		return
	}
	disks := schemaItem[0].(map[string]interface{})
	mapToStruct_QemuSataStorage(sata.Disk_0, "sata0", disks)
	mapToStruct_QemuSataStorage(sata.Disk_1, "sata1", disks)
	mapToStruct_QemuSataStorage(sata.Disk_2, "sata2", disks)
	mapToStruct_QemuSataStorage(sata.Disk_3, "sata3", disks)
	mapToStruct_QemuSataStorage(sata.Disk_4, "sata4", disks)
	mapToStruct_QemuSataStorage(sata.Disk_5, "sata5", disks)
}

func mapToStruct_QemuSataStorage(sata *pxapi.QemuSataStorage, key string, schema map[string]interface{}) {
	schemaItem, ok := schema[key].([]interface{})
	if !ok || len(schemaItem) != 1 || schemaItem[0] == nil {
		return
	}
	storageSchema := schemaItem[0].(map[string]interface{})
	tmpDisk, ok := storageSchema["disk"].([]interface{})
	if ok && len(tmpDisk) == 1 && tmpDisk[0] != nil {
		disk := tmpDisk[0].(map[string]interface{})
		sata.Disk = &pxapi.QemuSataDisk{
			Backup:          disk["backup"].(bool),
			Bandwidth:       mapToStruct_QemuDiskBandwidth(disk),
			Discard:         disk["discard"].(bool),
			EmulateSSD:      disk["emulatessd"].(bool),
			Format:          pxapi.QemuDiskFormat(disk["format"].(string)),
			Replicate:       disk["replicate"].(bool),
			SizeInKibibytes: pxapi.QemuDiskSize(convert_SizeStringToKibibytes_Unsafe(disk["size"].(string))),
			Storage:         disk["storage"].(string),
		}
		if asyncIO, ok := disk["asyncio"].(string); ok {
			sata.Disk.AsyncIO = pxapi.QemuDiskAsyncIO(asyncIO)
		}
		if cache, ok := disk["cache"].(string); ok {
			sata.Disk.Cache = pxapi.QemuDiskCache(cache)
		}
		if serial, ok := disk["serial"].(string); ok {
			sata.Disk.Serial = pxapi.QemuDiskSerial(serial)
		}
		return
	}
	tmpPassthrough, ok := storageSchema["passthrough"].([]interface{})
	if ok && len(tmpPassthrough) == 1 && tmpPassthrough[0] != nil {
		passthrough := tmpPassthrough[0].(map[string]interface{})
		sata.Passthrough = &pxapi.QemuSataPassthrough{
			Backup:     passthrough["backup"].(bool),
			Bandwidth:  mapToStruct_QemuDiskBandwidth(passthrough),
			Discard:    passthrough["discard"].(bool),
			EmulateSSD: passthrough["emulatessd"].(bool),
			File:       passthrough["file"].(string),
			Replicate:  passthrough["replicate"].(bool),
		}
		if asyncIO, ok := passthrough["asyncio"].(string); ok {
			sata.Passthrough.AsyncIO = pxapi.QemuDiskAsyncIO(asyncIO)
		}
		if cache, ok := passthrough["cache"].(string); ok {
			sata.Passthrough.Cache = pxapi.QemuDiskCache(cache)
		}
		if serial, ok := passthrough["serial"].(string); ok {
			sata.Passthrough.Serial = pxapi.QemuDiskSerial(serial)
		}
		return
	}
	sata.CdRom = mapToStruct_QemuCdRom(storageSchema)
}

func mapToStruct_QemuScsiDisks(scsi *pxapi.QemuScsiDisks, schema map[string]interface{}) {
	schemaItem, ok := schema["scsi"].([]interface{})
	if !ok || len(schemaItem) != 1 || schemaItem[0] == nil {
		return
	}
	disks := schemaItem[0].(map[string]interface{})
	mapToStruct_QemuScsiStorage(scsi.Disk_0, "scsi0", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_1, "scsi1", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_2, "scsi2", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_3, "scsi3", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_4, "scsi4", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_5, "scsi5", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_6, "scsi6", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_7, "scsi7", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_8, "scsi8", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_9, "scsi9", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_10, "scsi10", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_11, "scsi11", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_12, "scsi12", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_13, "scsi13", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_14, "scsi14", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_15, "scsi15", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_16, "scsi16", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_17, "scsi17", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_18, "scsi18", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_19, "scsi19", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_20, "scsi20", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_21, "scsi21", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_22, "scsi22", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_23, "scsi23", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_24, "scsi24", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_25, "scsi25", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_26, "scsi26", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_27, "scsi27", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_28, "scsi28", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_29, "scsi29", disks)
	mapToStruct_QemuScsiStorage(scsi.Disk_30, "scsi30", disks)
}

func mapToStruct_QemuScsiStorage(scsi *pxapi.QemuScsiStorage, key string, schema map[string]interface{}) {
	schemaItem, ok := schema[key].([]interface{})
	if !ok || len(schemaItem) != 1 || schemaItem[0] == nil {
		return
	}
	storageSchema := schemaItem[0].(map[string]interface{})
	tmpDisk, ok := storageSchema["disk"].([]interface{})
	if ok && len(tmpDisk) == 1 && tmpDisk[0] != nil {
		disk := tmpDisk[0].(map[string]interface{})
		scsi.Disk = &pxapi.QemuScsiDisk{
			Backup:          disk["backup"].(bool),
			Bandwidth:       mapToStruct_QemuDiskBandwidth(disk),
			Discard:         disk["discard"].(bool),
			EmulateSSD:      disk["emulatessd"].(bool),
			Format:          pxapi.QemuDiskFormat(disk["format"].(string)),
			IOThread:        disk["iothread"].(bool),
			ReadOnly:        disk["readonly"].(bool),
			Replicate:       disk["replicate"].(bool),
			SizeInKibibytes: pxapi.QemuDiskSize(convert_SizeStringToKibibytes_Unsafe(disk["size"].(string))),
			Storage:         disk["storage"].(string),
		}
		if asyncIO, ok := disk["asyncio"].(string); ok {
			scsi.Disk.AsyncIO = pxapi.QemuDiskAsyncIO(asyncIO)
		}
		if cache, ok := disk["cache"].(string); ok {
			scsi.Disk.Cache = pxapi.QemuDiskCache(cache)
		}
		if serial, ok := disk["serial"].(string); ok {
			scsi.Disk.Serial = pxapi.QemuDiskSerial(serial)
		}
		return
	}
	tmpPassthrough, ok := storageSchema["passthrough"].([]interface{})
	if ok && len(tmpPassthrough) == 1 && tmpPassthrough[0] != nil {
		passthrough := tmpPassthrough[0].(map[string]interface{})
		scsi.Passthrough = &pxapi.QemuScsiPassthrough{
			Backup:     passthrough["backup"].(bool),
			Bandwidth:  mapToStruct_QemuDiskBandwidth(passthrough),
			Discard:    passthrough["discard"].(bool),
			EmulateSSD: passthrough["emulatessd"].(bool),
			File:       passthrough["file"].(string),
			IOThread:   passthrough["iothread"].(bool),
			ReadOnly:   passthrough["readonly"].(bool),
			Replicate:  passthrough["replicate"].(bool),
		}
		if asyncIO, ok := passthrough["asyncio"].(string); ok {
			scsi.Passthrough.AsyncIO = pxapi.QemuDiskAsyncIO(asyncIO)
		}
		if cache, ok := passthrough["cache"].(string); ok {
			scsi.Passthrough.Cache = pxapi.QemuDiskCache(cache)
		}
		if serial, ok := passthrough["serial"].(string); ok {
			scsi.Passthrough.Serial = pxapi.QemuDiskSerial(serial)
		}
		return
	}
	scsi.CdRom = mapToStruct_QemuCdRom(storageSchema)
}

func mapToStruct_QemuStorages(d *schema.ResourceData) *pxapi.QemuStorages {
	storages := pxapi.QemuStorages{
		Ide: &pxapi.QemuIdeDisks{
			Disk_0: &pxapi.QemuIdeStorage{},
			Disk_1: &pxapi.QemuIdeStorage{},
			Disk_2: &pxapi.QemuIdeStorage{},
			Disk_3: &pxapi.QemuIdeStorage{},
		},
		Sata: &pxapi.QemuSataDisks{
			Disk_0: &pxapi.QemuSataStorage{},
			Disk_1: &pxapi.QemuSataStorage{},
			Disk_2: &pxapi.QemuSataStorage{},
			Disk_3: &pxapi.QemuSataStorage{},
			Disk_4: &pxapi.QemuSataStorage{},
			Disk_5: &pxapi.QemuSataStorage{},
		},
		Scsi: &pxapi.QemuScsiDisks{
			Disk_0:  &pxapi.QemuScsiStorage{},
			Disk_1:  &pxapi.QemuScsiStorage{},
			Disk_2:  &pxapi.QemuScsiStorage{},
			Disk_3:  &pxapi.QemuScsiStorage{},
			Disk_4:  &pxapi.QemuScsiStorage{},
			Disk_5:  &pxapi.QemuScsiStorage{},
			Disk_6:  &pxapi.QemuScsiStorage{},
			Disk_7:  &pxapi.QemuScsiStorage{},
			Disk_8:  &pxapi.QemuScsiStorage{},
			Disk_9:  &pxapi.QemuScsiStorage{},
			Disk_10: &pxapi.QemuScsiStorage{},
			Disk_11: &pxapi.QemuScsiStorage{},
			Disk_12: &pxapi.QemuScsiStorage{},
			Disk_13: &pxapi.QemuScsiStorage{},
			Disk_14: &pxapi.QemuScsiStorage{},
			Disk_15: &pxapi.QemuScsiStorage{},
			Disk_16: &pxapi.QemuScsiStorage{},
			Disk_17: &pxapi.QemuScsiStorage{},
			Disk_18: &pxapi.QemuScsiStorage{},
			Disk_19: &pxapi.QemuScsiStorage{},
			Disk_20: &pxapi.QemuScsiStorage{},
			Disk_21: &pxapi.QemuScsiStorage{},
			Disk_22: &pxapi.QemuScsiStorage{},
			Disk_23: &pxapi.QemuScsiStorage{},
			Disk_24: &pxapi.QemuScsiStorage{},
			Disk_25: &pxapi.QemuScsiStorage{},
			Disk_26: &pxapi.QemuScsiStorage{},
			Disk_27: &pxapi.QemuScsiStorage{},
			Disk_28: &pxapi.QemuScsiStorage{},
			Disk_29: &pxapi.QemuScsiStorage{},
			Disk_30: &pxapi.QemuScsiStorage{},
		},
		VirtIO: &pxapi.QemuVirtIODisks{
			Disk_0:  &pxapi.QemuVirtIOStorage{},
			Disk_1:  &pxapi.QemuVirtIOStorage{},
			Disk_2:  &pxapi.QemuVirtIOStorage{},
			Disk_3:  &pxapi.QemuVirtIOStorage{},
			Disk_4:  &pxapi.QemuVirtIOStorage{},
			Disk_5:  &pxapi.QemuVirtIOStorage{},
			Disk_6:  &pxapi.QemuVirtIOStorage{},
			Disk_7:  &pxapi.QemuVirtIOStorage{},
			Disk_8:  &pxapi.QemuVirtIOStorage{},
			Disk_9:  &pxapi.QemuVirtIOStorage{},
			Disk_10: &pxapi.QemuVirtIOStorage{},
			Disk_11: &pxapi.QemuVirtIOStorage{},
			Disk_12: &pxapi.QemuVirtIOStorage{},
			Disk_13: &pxapi.QemuVirtIOStorage{},
			Disk_14: &pxapi.QemuVirtIOStorage{},
			Disk_15: &pxapi.QemuVirtIOStorage{},
		},
	}
	schemaItem := d.Get("disks").([]interface{})
	if len(schemaItem) == 1 {
		schemaStorages, ok := schemaItem[0].(map[string]interface{})
		if ok {
			mapToStruct_QemuIdeDisks(storages.Ide, schemaStorages)
			mapToStruct_QemuSataDisks(storages.Sata, schemaStorages)
			mapToStruct_QemuScsiDisks(storages.Scsi, schemaStorages)
			mapToStruct_QemuVirtIODisks(storages.VirtIO, schemaStorages)
		}
	}
	return &storages
}

func mapToStruct_QemuVirtIODisks(virtio *pxapi.QemuVirtIODisks, schema map[string]interface{}) {
	schemaItem, ok := schema["virtio"].([]interface{})
	if !ok || len(schemaItem) != 1 || schemaItem[0] == nil {
		return
	}
	disks := schemaItem[0].(map[string]interface{})
	mapToStruct_VirtIOStorage(virtio.Disk_0, "virtio0", disks)
	mapToStruct_VirtIOStorage(virtio.Disk_1, "virtio1", disks)
	mapToStruct_VirtIOStorage(virtio.Disk_2, "virtio2", disks)
	mapToStruct_VirtIOStorage(virtio.Disk_3, "virtio3", disks)
	mapToStruct_VirtIOStorage(virtio.Disk_4, "virtio4", disks)
	mapToStruct_VirtIOStorage(virtio.Disk_5, "virtio5", disks)
	mapToStruct_VirtIOStorage(virtio.Disk_6, "virtio6", disks)
	mapToStruct_VirtIOStorage(virtio.Disk_7, "virtio7", disks)
	mapToStruct_VirtIOStorage(virtio.Disk_8, "virtio8", disks)
	mapToStruct_VirtIOStorage(virtio.Disk_9, "virtio9", disks)
	mapToStruct_VirtIOStorage(virtio.Disk_10, "virtio10", disks)
	mapToStruct_VirtIOStorage(virtio.Disk_11, "virtio11", disks)
	mapToStruct_VirtIOStorage(virtio.Disk_12, "virtio12", disks)
	mapToStruct_VirtIOStorage(virtio.Disk_13, "virtio13", disks)
	mapToStruct_VirtIOStorage(virtio.Disk_14, "virtio14", disks)
	mapToStruct_VirtIOStorage(virtio.Disk_15, "virtio15", disks)
}

func mapToStruct_VirtIOStorage(virtio *pxapi.QemuVirtIOStorage, key string, schema map[string]interface{}) {
	schemaItem, ok := schema[key].([]interface{})
	if !ok || len(schemaItem) != 1 || schemaItem[0] == nil {
		return
	}
	storageSchema := schemaItem[0].(map[string]interface{})
	tmpDisk, ok := storageSchema["disk"].([]interface{})
	if ok && len(tmpDisk) == 1 && tmpDisk[0] != nil {
		disk := tmpDisk[0].(map[string]interface{})
		virtio.Disk = &pxapi.QemuVirtIODisk{
			Backup:          disk["backup"].(bool),
			Bandwidth:       mapToStruct_QemuDiskBandwidth(disk),
			Discard:         disk["discard"].(bool),
			Format:          pxapi.QemuDiskFormat(disk["format"].(string)),
			IOThread:        disk["iothread"].(bool),
			ReadOnly:        disk["readonly"].(bool),
			Replicate:       disk["replicate"].(bool),
			SizeInKibibytes: pxapi.QemuDiskSize(convert_SizeStringToKibibytes_Unsafe(disk["size"].(string))),
			Storage:         disk["storage"].(string),
		}
		if asyncIO, ok := disk["asyncio"].(string); ok {
			virtio.Disk.AsyncIO = pxapi.QemuDiskAsyncIO(asyncIO)
		}
		if cache, ok := disk["cache"].(string); ok {
			virtio.Disk.Cache = pxapi.QemuDiskCache(cache)
		}
		if serial, ok := disk["serial"].(string); ok {
			virtio.Disk.Serial = pxapi.QemuDiskSerial(serial)
		}
		return
	}
	tmpPassthrough, ok := storageSchema["passthrough"].([]interface{})
	if ok && len(tmpPassthrough) == 1 && tmpPassthrough[0] != nil {
		passthrough := tmpPassthrough[0].(map[string]interface{})
		virtio.Passthrough = &pxapi.QemuVirtIOPassthrough{
			Backup:    passthrough["backup"].(bool),
			Bandwidth: mapToStruct_QemuDiskBandwidth(passthrough),
			Discard:   passthrough["discard"].(bool),
			File:      passthrough["file"].(string),
			IOThread:  passthrough["iothread"].(bool),
			ReadOnly:  passthrough["readonly"].(bool),
			Replicate: passthrough["replicate"].(bool),
		}
		if asyncIO, ok := passthrough["asyncio"].(string); ok {
			virtio.Passthrough.AsyncIO = pxapi.QemuDiskAsyncIO(asyncIO)
		}
		if cache, ok := passthrough["cache"].(string); ok {
			virtio.Passthrough.Cache = pxapi.QemuDiskCache(cache)
		}
		if serial, ok := passthrough["serial"].(string); ok {
			virtio.Passthrough.Serial = pxapi.QemuDiskSerial(serial)
		}
		return
	}
	virtio.CdRom = mapToStruct_QemuCdRom(storageSchema)
}

// schema definition
func schema_CdRom(path string) *schema.Schema {
	return &schema.Schema{
		Type:          schema.TypeList,
		Optional:      true,
		MaxItems:      1,
		ConflictsWith: []string{path + ".disk", path + ".passthrough"},
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"iso": {
					Type:          schema.TypeString,
					Optional:      true,
					ConflictsWith: []string{path + ".cdrom.0.passthrough"},
				},
				"passthrough": {
					Type:          schema.TypeBool,
					Optional:      true,
					ConflictsWith: []string{path + ".cdrom.0.iso"},
				},
			},
		},
	}
}

func schema_Ide(setting string) *schema.Schema {
	path := "disks.0.ide.0." + setting + ".0"
	return &schema.Schema{
		Type:     schema.TypeList,
		Optional: true,
		MaxItems: 1,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"cdrom": schema_CdRom(path),
				"disk": {
					Type:          schema.TypeList,
					Optional:      true,
					MaxItems:      1,
					ConflictsWith: []string{path + ".cdrom", path + ".passthrough"},
					Elem: &schema.Resource{
						Schema: schema_DiskBandwidth(map[string]*schema.Schema{
							"asyncio":        schema_DiskAsyncIO(),
							"backup":         schema_DiskBackup(),
							"cache":          schema_DiskCache(),
							"discard":        {Type: schema.TypeBool, Optional: true},
							"emulatessd":     {Type: schema.TypeBool, Optional: true},
							"format":         schema_DiskFormat(),
							"id":             schema_DiskId(),
							"linked_disk_id": schema_LinkedDiskId(),
							"replicate":      {Type: schema.TypeBool, Optional: true},
							"serial":         schema_DiskSerial(),
							"size":           schema_DiskSize(),
							"storage":        schema_DiskStorage(),
							"wwn":            schema_DiskWWN(),
						}),
					},
				},
				"passthrough": {
					Type:          schema.TypeList,
					Optional:      true,
					MaxItems:      1,
					ConflictsWith: []string{path + ".cdrom", path + ".disk"},
					Elem: &schema.Resource{
						Schema: schema_DiskBandwidth(map[string]*schema.Schema{
							"asyncio":    schema_DiskAsyncIO(),
							"backup":     schema_DiskBackup(),
							"cache":      schema_DiskCache(),
							"discard":    {Type: schema.TypeBool, Optional: true},
							"emulatessd": {Type: schema.TypeBool, Optional: true},
							"file":       schema_PassthroughFile(),
							"replicate":  {Type: schema.TypeBool, Optional: true},
							"serial":     schema_DiskSerial(),
							"size":       schema_PassthroughSize(),
							"wwn":        schema_DiskWWN(),
						}),
					},
				},
			},
		},
	}
}

func schema_Sata(setting string) *schema.Schema {
	path := "disks.0.sata.0." + setting + ".0"
	return &schema.Schema{
		Type:     schema.TypeList,
		Optional: true,
		MaxItems: 1,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"cdrom": schema_CdRom(path),
				"disk": {
					Type:          schema.TypeList,
					Optional:      true,
					MaxItems:      1,
					ConflictsWith: []string{path + ".cdrom", path + ".passthrough"},
					Elem: &schema.Resource{
						Schema: schema_DiskBandwidth(map[string]*schema.Schema{
							"asyncio":        schema_DiskAsyncIO(),
							"backup":         schema_DiskBackup(),
							"cache":          schema_DiskCache(),
							"discard":        {Type: schema.TypeBool, Optional: true},
							"emulatessd":     {Type: schema.TypeBool, Optional: true},
							"format":         schema_DiskFormat(),
							"id":             schema_DiskId(),
							"linked_disk_id": schema_LinkedDiskId(),
							"replicate":      {Type: schema.TypeBool, Optional: true},
							"serial":         schema_DiskSerial(),
							"size":           schema_DiskSize(),
							"storage":        schema_DiskStorage(),
							"wwn":            schema_DiskWWN(),
						}),
					},
				},
				"passthrough": {
					Type:          schema.TypeList,
					Optional:      true,
					MaxItems:      1,
					ConflictsWith: []string{path + ".cdrom", path + ".disk"},
					Elem: &schema.Resource{
						Schema: schema_DiskBandwidth(map[string]*schema.Schema{
							"asyncio":    schema_DiskAsyncIO(),
							"backup":     schema_DiskBackup(),
							"cache":      schema_DiskCache(),
							"discard":    {Type: schema.TypeBool, Optional: true},
							"emulatessd": {Type: schema.TypeBool, Optional: true},
							"file":       schema_PassthroughFile(),
							"replicate":  {Type: schema.TypeBool, Optional: true},
							"serial":     schema_DiskSerial(),
							"size":       schema_PassthroughSize(),
							"wwn":        schema_DiskWWN(),
						}),
					},
				},
			},
		},
	}
}

func schema_Scsi(setting string) *schema.Schema {
	path := "disks.0.scsi.0." + setting + ".0"
	return &schema.Schema{
		Type:     schema.TypeList,
		Optional: true,
		MaxItems: 1,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"cdrom": schema_CdRom(path),
				"disk": {
					Type:          schema.TypeList,
					Optional:      true,
					MaxItems:      1,
					ConflictsWith: []string{path + ".cdrom", path + ".passthrough"},
					Elem: &schema.Resource{
						Schema: schema_DiskBandwidth(map[string]*schema.Schema{
							"asyncio":        schema_DiskAsyncIO(),
							"backup":         schema_DiskBackup(),
							"cache":          schema_DiskCache(),
							"discard":        {Type: schema.TypeBool, Optional: true},
							"emulatessd":     {Type: schema.TypeBool, Optional: true},
							"format":         schema_DiskFormat(),
							"id":             schema_DiskId(),
							"iothread":       {Type: schema.TypeBool, Optional: true},
							"linked_disk_id": schema_LinkedDiskId(),
							"readonly":       {Type: schema.TypeBool, Optional: true},
							"replicate":      {Type: schema.TypeBool, Optional: true},
							"serial":         schema_DiskSerial(),
							"size":           schema_DiskSize(),
							"storage":        schema_DiskStorage(),
							"wwn":            schema_DiskWWN(),
						}),
					},
				},
				"passthrough": {
					Type:          schema.TypeList,
					Optional:      true,
					MaxItems:      1,
					ConflictsWith: []string{path + ".cdrom", path + ".disk"},
					Elem: &schema.Resource{
						Schema: schema_DiskBandwidth(map[string]*schema.Schema{
							"asyncio":    schema_DiskAsyncIO(),
							"backup":     schema_DiskBackup(),
							"cache":      schema_DiskCache(),
							"discard":    {Type: schema.TypeBool, Optional: true},
							"emulatessd": {Type: schema.TypeBool, Optional: true},
							"file":       schema_PassthroughFile(),
							"iothread":   {Type: schema.TypeBool, Optional: true},
							"readonly":   {Type: schema.TypeBool, Optional: true},
							"replicate":  {Type: schema.TypeBool, Optional: true},
							"serial":     schema_DiskSerial(),
							"size":       schema_PassthroughSize(),
							"wwn":        schema_DiskWWN(),
						}),
					},
				},
			},
		},
	}
}

func schema_Virtio(setting string) *schema.Schema {
	path := "disks.0.virtio.0." + setting + ".0"
	return &schema.Schema{
		Type:     schema.TypeList,
		Optional: true,
		MaxItems: 1,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"cdrom": schema_CdRom(path),
				"disk": {
					Type:          schema.TypeList,
					Optional:      true,
					MaxItems:      1,
					ConflictsWith: []string{path + ".cdrom", path + ".passthrough"},
					Elem: &schema.Resource{
						Schema: schema_DiskBandwidth(map[string]*schema.Schema{
							"asyncio":        schema_DiskAsyncIO(),
							"backup":         schema_DiskBackup(),
							"cache":          schema_DiskCache(),
							"discard":        {Type: schema.TypeBool, Optional: true},
							"format":         schema_DiskFormat(),
							"id":             schema_DiskId(),
							"iothread":       {Type: schema.TypeBool, Optional: true},
							"linked_disk_id": schema_LinkedDiskId(),
							"readonly":       {Type: schema.TypeBool, Optional: true},
							"replicate":      {Type: schema.TypeBool, Optional: true},
							"serial":         schema_DiskSerial(),
							"size":           schema_DiskSize(),
							"storage":        schema_DiskStorage(),
							"wwn":            schema_DiskWWN(),
						}),
					},
				},
				"passthrough": {
					Type:          schema.TypeList,
					Optional:      true,
					MaxItems:      1,
					ConflictsWith: []string{path + ".cdrom", path + ".disk"},
					Elem: &schema.Resource{Schema: schema_DiskBandwidth(
						map[string]*schema.Schema{
							"asyncio":   schema_DiskAsyncIO(),
							"backup":    schema_DiskBackup(),
							"cache":     schema_DiskCache(),
							"discard":   {Type: schema.TypeBool, Optional: true},
							"file":      schema_PassthroughFile(),
							"iothread":  {Type: schema.TypeBool, Optional: true},
							"readonly":  {Type: schema.TypeBool, Optional: true},
							"replicate": {Type: schema.TypeBool, Optional: true},
							"serial":    schema_DiskSerial(),
							"size":      schema_PassthroughSize(),
							"wwn":       schema_DiskWWN(),
						},
					)},
				},
			},
		},
	}
}

func schema_DiskAsyncIO() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeString,
		Optional: true,
		ValidateDiagFunc: func(i interface{}, k cty.Path) diag.Diagnostics {
			v, ok := i.(string)
			if !ok {
				return diag.Errorf(errorString, k)
			}
			if err := pxapi.QemuDiskAsyncIO(v).Validate(); err != nil {
				return diag.FromErr(err)
			}
			return nil
		},
	}
}

func schema_DiskBackup() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeBool,
		Optional: true,
		Default:  true,
	}
}

func schema_DiskBandwidth(params map[string]*schema.Schema) map[string]*schema.Schema {
	params["mbps_r_burst"] = schema_DiskBandwidthMBpsBurst()
	params["mbps_r_concurrent"] = schema_DiskBandwidthMBpsConcurrent()
	params["mbps_wr_burst"] = schema_DiskBandwidthMBpsBurst()
	params["mbps_wr_concurrent"] = schema_DiskBandwidthMBpsConcurrent()
	params["iops_r_burst"] = schema_DiskBandwidthIopsBurst()
	params["iops_r_burst_length"] = schema_DiskBandwidthIopsBurstLength()
	params["iops_r_concurrent"] = schema_DiskBandwidthIopsConcurrent()
	params["iops_wr_burst"] = schema_DiskBandwidthIopsBurst()
	params["iops_wr_burst_length"] = schema_DiskBandwidthIopsBurstLength()
	params["iops_wr_concurrent"] = schema_DiskBandwidthIopsConcurrent()
	return params
}

func schema_DiskBandwidthIopsBurst() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeInt,
		Optional: true,
		Default:  0,
		ValidateDiagFunc: func(i interface{}, k cty.Path) diag.Diagnostics {
			v, ok := i.(int)
			if !ok || v < 0 {
				return diag.Errorf(errorUint, k)
			}
			if err := pxapi.QemuDiskBandwidthIopsLimitBurst(v).Validate(); err != nil {
				return diag.FromErr(err)
			}
			return nil
		},
	}
}

func schema_DiskBandwidthIopsBurstLength() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeInt,
		Optional: true,
		Default:  0,
	}
}

func schema_DiskBandwidthIopsConcurrent() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeInt,
		Optional: true,
		Default:  0,
		ValidateDiagFunc: func(i interface{}, k cty.Path) diag.Diagnostics {
			v, ok := i.(int)
			if !ok || v < 0 {
				return diag.Errorf(errorUint, k)
			}
			if err := pxapi.QemuDiskBandwidthIopsLimitConcurrent(v).Validate(); err != nil {
				return diag.FromErr(err)
			}
			return nil
		},
	}
}

func schema_DiskBandwidthMBpsBurst() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeFloat,
		Optional: true,
		Default:  0.0,
		ValidateDiagFunc: func(i interface{}, k cty.Path) diag.Diagnostics {
			v, ok := i.(float64)
			if !ok {
				return diag.Errorf(errorFloat, k)
			}
			if err := pxapi.QemuDiskBandwidthMBpsLimitBurst(v).Validate(); err != nil {
				return diag.FromErr(err)
			}
			return nil
		},
	}
}

func schema_DiskBandwidthMBpsConcurrent() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeFloat,
		Optional: true,
		Default:  0.0,
		ValidateDiagFunc: func(i interface{}, k cty.Path) diag.Diagnostics {
			v, ok := i.(float64)
			if !ok {
				return diag.Errorf(errorFloat, k)
			}
			if err := pxapi.QemuDiskBandwidthMBpsLimitConcurrent(v).Validate(); err != nil {
				return diag.FromErr(err)
			}
			return nil
		},
	}
}

func schema_DiskCache() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeString,
		Optional: true,
		Default:  "",
		ValidateDiagFunc: func(i interface{}, k cty.Path) diag.Diagnostics {
			v, ok := i.(string)
			if !ok {
				return diag.Errorf(errorString, k)
			}
			if err := pxapi.QemuDiskCache(v).Validate(); err != nil {
				return diag.FromErr(err)
			}
			return nil
		},
	}
}

func schema_DiskFormat() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeString,
		Optional: true,
		Default:  "raw",
		ValidateDiagFunc: func(i interface{}, k cty.Path) diag.Diagnostics {
			v, ok := i.(string)
			if !ok {
				return diag.Errorf(errorString, k)
			}
			if err := pxapi.QemuDiskFormat(v).Validate(); err != nil {
				return diag.FromErr(err)
			}
			return nil
		},
	}
}

func schema_DiskId() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeInt,
		Computed: true,
	}
}

func schema_DiskSerial() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeString,
		Optional: true,
		Default:  "",
		ValidateDiagFunc: func(i interface{}, k cty.Path) diag.Diagnostics {
			v, ok := i.(string)
			if !ok {
				return diag.Errorf(errorString, k)
			}
			if err := pxapi.QemuDiskSerial(v).Validate(); err != nil {
				return diag.FromErr(err)
			}
			return nil
		},
	}
}

func schema_DiskSize() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeString,
		Required: true,
		ValidateDiagFunc: func(i interface{}, k cty.Path) diag.Diagnostics {
			v, ok := i.(string)
			if !ok {
				return diag.Errorf(errorString, k)
			}
			if !regexp.MustCompile(`^[123456789]\d*[KMGT]?$`).MatchString(v) {
				return diag.Errorf("%s must match the following regex ^[123456789]\\d*[KMGT]?$", k)
			}
			return nil
		},
		DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
			return convert_SizeStringToKibibytes_Unsafe(old) == convert_SizeStringToKibibytes_Unsafe(new)
		},
	}
}

func schema_DiskStorage() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeString,
		Required: true,
	}
}

func schema_DiskWWN() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeString,
		Optional: true,
		ValidateDiagFunc: func(i interface{}, k cty.Path) diag.Diagnostics {
			v, ok := i.(string)
			if !ok {
				return diag.Errorf(errorString, k)
			}
			if err := pxapi.QemuWorldWideName(v).Validate(); err != nil {
				return diag.FromErr(err)
			}
			return nil
		},
	}
}

func schema_LinkedDiskId() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeInt,
		Computed: true,
	}
}

func schema_PassthroughFile() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeString,
		Required: true,
	}
}

func schema_PassthroughSize() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeString,
		Computed: true,
	}
}
