package provider

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourceCreateVm(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {

	// pconf := meta.(*providerConfiguration)
	// vmName := d.Get("name").(string)
	// client := pconf.Client
	// vga := d.Get("vga").(*schema.Set)
	// qemuVgaList := vga.List()
	// qemuNetworks, _ := ExpandDevicesList(d.Get("network").([]interface{}))
	// qemuEfiDisks, _ := ExpandDevicesList(d.Get("efidisk").([]interface{}))
	// serials := d.Get("serial").(*schema.Set)
	// qemuSerials, _ := DevicesSetToMap(serials)

	// qemuPCIDevices, _ := ExpandDevicesList(d.Get("hostpci").([]interface{}))

	// qemuUsbs, _ := ExpandDevicesList(d.Get("usb").([]interface{}))

	// config := pxapi.ConfigQemu{
	// 	Name:           vmName,
	// 	Description:    d.Get("desc").(string),
	// 	Pool:           d.Get("pool").(string),
	// 	Bios:           d.Get("bios").(string),
	// 	Onboot:         BoolPointer(d.Get("onboot").(bool)),
	// 	Startup:        d.Get("startup").(string),
	// 	Protection:     BoolPointer(d.Get("protection").(bool)),
	// 	Tablet:         BoolPointer(d.Get("tablet").(bool)),
	// 	Boot:           d.Get("boot").(string),
	// 	BootDisk:       d.Get("bootdisk").(string),
	// 	Agent:          mapToStruct_QemuGuestAgent(d),
	// 	Memory:         d.Get("memory").(int),
	// 	Machine:        d.Get("machine").(string),
	// 	Balloon:        d.Get("balloon").(int),
	// 	QemuCores:      d.Get("cores").(int),
	// 	QemuSockets:    d.Get("sockets").(int),
	// 	QemuVcpus:      d.Get("vcpus").(int),
	// 	QemuCpu:        d.Get("cpu").(string),
	// 	QemuNuma:       BoolPointer(d.Get("numa").(bool)),
	// 	QemuKVM:        BoolPointer(d.Get("kvm").(bool)),
	// 	Hotplug:        d.Get("hotplug").(string),
	// 	Scsihw:         d.Get("scsihw").(string),
	// 	HaState:        d.Get("hastate").(string),
	// 	HaGroup:        d.Get("hagroup").(string),
	// 	QemuOs:         d.Get("qemu_os").(string),
	// 	Tags:           d.Get("tags").(string),
	// 	Args:           d.Get("args").(string),
	// 	QemuNetworks:   qemuNetworks,
	// 	QemuSerials:    qemuSerials,
	// 	QemuPCIDevices: qemuPCIDevices,
	// 	QemuUsbs:       qemuUsbs,
	// 	Smbios1:        BuildSmbiosArgs(d.Get("smbios").([]interface{})),
	// 	// Cloud-init.
	// 	CIuser:       d.Get("ciuser").(string),
	// 	CIpassword:   d.Get("cipassword").(string),
	// 	CIcustom:     d.Get("cicustom").(string),
	// 	Searchdomain: d.Get("searchdomain").(string),
	// 	Nameserver:   d.Get("nameserver").(string),
	// 	Sshkeys:      d.Get("sshkeys").(string),
	// 	Ipconfig:     pxapi.IpconfigMap{},
	// }
	// // Populate Ipconfig map
	// for i := 0; i < 16; i++ {
	// 	iface := fmt.Sprintf("ipconfig%d", i)
	// 	if v, ok := d.GetOk(iface); ok {
	// 		config.Ipconfig[i] = v.(string)
	// 	}
	// }

	// config.Disks = mapToStruct_QemuStorages(d)
	// setCloudInitDisk(d, &config)

	// if len(qemuVgaList) > 0 {
	// 	config.QemuVga = qemuVgaList[0].(map[string]interface{})
	// }

	// if len(qemuEfiDisks) > 0 {
	// 	config.EFIDisk = qemuEfiDisks[0]
	// }

	// log.Printf("[DEBUG][QemuVmCreate] checking for duplicate name: %s", vmName)
	// dupVmr, _ := client.GetVmRefByName(vmName)

	// forceCreate := d.Get("force_create").(bool)

	// targetNodesRaw := d.Get("target_nodes").([]interface{})
	// var targetNodes = make([]string, len(targetNodesRaw))
	// for i, raw := range targetNodesRaw {
	// 	targetNodes[i] = raw.(string)
	// }

	// var targetNode string

	// if len(targetNodes) == 0 {
	// 	targetNode = d.Get("target_node").(string)
	// } else {
	// 	targetNode = targetNodes[rand.Intn(len(targetNodes))]
	// }

	// if targetNode == "" {
	// 	return diag.FromErr(fmt.Errorf("VM name (%s) has no target node! Please use target_node or target_nodes to set a specific node! %v", vmName, targetNodes))
	// }
	// if dupVmr != nil && forceCreate {
	// 	return diag.FromErr(fmt.Errorf("duplicate VM name (%s) with vmId: %d. Set force_create=false to recycle", vmName, dupVmr.VmId()))
	// } else if dupVmr != nil && dupVmr.Node() != targetNode {
	// 	return diag.FromErr(fmt.Errorf("duplicate VM name (%s) with vmId: %d on different target_node=%s", vmName, dupVmr.VmId(), dupVmr.Node()))
	// }

	// vmr := dupVmr

	// // var rebootRequired bool
	// // var err error

	// d.SetId(resourceId(targetNode, "qemu", vmr.VmId()))
	log.Print("cretae this one")
	var diags diag.Diagnostics
	return diags
}
