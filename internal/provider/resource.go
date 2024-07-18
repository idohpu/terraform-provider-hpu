package provider

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Telmate/terraform-provider-proxmox/v2/proxmox"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

// var thisResource *schema.Resource

func BoolPointer(b bool) *bool {
	return &b
}

const (
	stateRunning string = "running"
	stateStarted string = "started"
	stateStopped string = "stopped"
)

func resourceVm() *schema.Resource {
	thisResource := &schema.Resource{
		CreateContext: resourceCreateVm,
		ReadContext:   resourceReadVm,
		UpdateContext: resourceUpdateVm,
		DeleteContext: resourceDeleteVm,
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},
		Schema: map[string]*schema.Schema{
			"agent": {
				Type:     schema.TypeInt,
				Optional: true,
				Default:  0,
			},
			"vmid": {
				Type:             schema.TypeInt,
				Optional:         true,
				Computed:         true,
				ForceNew:         true,
				ValidateDiagFunc: proxmox.VMIDValidator(),
				Description:      "The VM identifier in proxmox (100-999999999)",
			},
			"name": {
				Type:     schema.TypeString,
				Optional: true,
				// Default:     "",
				Description: "The VM name",
				ValidateFunc: func(val interface{}, key string) (warns []string, errs []error) {
					v := val.(string)
					matched, err := regexp.Match("[^a-zA-Z0-9-.]", []byte(v))
					if err != nil {
						warns = append(warns, fmt.Sprintf("%q, had an error running regexp.Match err=[%v]", key, err))
					}
					if matched {
						errs = append(errs, fmt.Errorf("%q, must only contain alphanumerics, hyphens and dots [%v]", key, v))
					}
					return
				},
			},
			"desc": {
				Type:     schema.TypeString,
				Optional: true,
				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					return strings.TrimSpace(old) == strings.TrimSpace(new)
				},
				// Default:     "",
				Description: "The VM description",
			},
			"target_node": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The node where VM goes to",
			},
			"target_nodes": {
				Type:     schema.TypeList,
				Optional: true,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Description: "A list of nodes where VM goes to",
			},
			"bios": {
				Type:             schema.TypeString,
				Optional:         true,
				Default:          "seabios",
				Description:      "The VM bios, it can be seabios or ovmf",
				ValidateDiagFunc: proxmox.BIOSValidator(),
			},
			"vm_state": {
				Type:             schema.TypeString,
				Optional:         true,
				Default:          stateRunning,
				Description:      "The state of the VM (" + stateRunning + ", " + stateStarted + ", " + stateStopped + ")",
				ValidateDiagFunc: proxmox.VMStateValidator(),
				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					return new == stateStarted
				},
			},
			"onboot": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "VM autostart on boot",
			},
			"startup": {
				Type:     schema.TypeString,
				Optional: true,
				// Default:     "",
				Description: "Startup order of the VM",
			},
			"protection": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Protect VM from being removed",
			},
			"tablet": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "Enable tablet mode in the VM",
			},
			"boot": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Boot order of the VM",
			},
			"bootdisk": {
				Type:     schema.TypeString,
				Computed: true,
				Optional: true,
			},
			"pxe": {
				Type:          schema.TypeBool,
				Optional:      true,
				ForceNew:      true,
				ConflictsWith: []string{"clone"},
			},
			"clone": {
				Type:          schema.TypeString,
				Optional:      true,
				ForceNew:      true,
				ConflictsWith: []string{"pxe"},
			},
			"cloudinit_cdrom_storage": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"full_clone": {
				Type:     schema.TypeBool,
				Optional: true,
				ForceNew: true,
				Default:  true,
			},
			"hastate": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"hagroup": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"qemu_os": {
				Type:     schema.TypeString,
				Optional: true,
				// Default:  "l26",
				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					// 	if new == "l26" {
					// 		return len(d.Get("clone").(string)) > 0 // the cloned source may have a different os, which we should leave alone
					// 	}
					if new == "" {
						return true
					}
					return strings.TrimSpace(old) == strings.TrimSpace(new)
				},
			},
			"tags": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},
			"args": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"machine": {
				Type:             schema.TypeString,
				Optional:         true,
				Description:      "Specifies the Qemu machine type.",
				ValidateDiagFunc: proxmox.MachineTypeValidator(),
				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					if old == new || (old != "" && new == "") {
						return true
					}
					return false
				},
			},
			"memory": {
				Type:     schema.TypeInt,
				Optional: true,
				Default:  512,
			},
			"balloon": {
				Type:     schema.TypeInt,
				Optional: true,
				Default:  0,
			},
			"cores": {
				Type:     schema.TypeInt,
				Optional: true,
				Default:  1,
			},
			"sockets": {
				Type:     schema.TypeInt,
				Optional: true,
				Default:  1,
			},
			"vcpus": {
				Type:     schema.TypeInt,
				Optional: true,
				Default:  0,
			},
			"cpu": {
				Type:     schema.TypeString,
				Optional: true,
				Default:  "host",
			},
			"numa": {
				Type:     schema.TypeBool,
				Optional: true,
				// Default:  false,
			},
			"kvm": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  true,
			},
			"hotplug": {
				Type:     schema.TypeString,
				Optional: true,
				Default:  "network,disk,usb",
			},
			"scsihw": {
				Type:     schema.TypeString,
				Optional: true,
				Default:  "lsi",
				ValidateFunc: validation.StringInSlice([]string{
					"lsi",
					"lsi53c810",
					"virtio-scsi-pci",
					"virtio-scsi-single",
					"megasas",
					"pvscsi",
				}, false),
			},
			"vga": {
				Type:     schema.TypeSet,
				Optional: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"type": {
							Type:     schema.TypeString,
							Optional: true,
							Default:  "std",
						},
						"memory": {
							Type:     schema.TypeInt,
							Optional: true,
						},
					},
				},
			},
			"network": {
				Type:     schema.TypeList,
				Optional: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"model": {
							Type:     schema.TypeString,
							Required: true,
						},
						"macaddr": {
							Type:             schema.TypeString,
							Optional:         true,
							Computed:         true,
							ValidateDiagFunc: proxmox.MacAddressValidator(),
						},
						"bridge": {
							Type:     schema.TypeString,
							Optional: true,
							Default:  "nat",
						},
						"tag": {
							Type:        schema.TypeInt,
							Optional:    true,
							Description: "VLAN tag.",
							Default:     -1,
						},
						"firewall": {
							Type:     schema.TypeBool,
							Optional: true,
							Default:  false,
						},
						"rate": {
							Type:     schema.TypeInt,
							Optional: true,
							Computed: true,
						},
						"mtu": {
							Type:     schema.TypeInt,
							Optional: true,
						},
						"queues": {
							Type:     schema.TypeInt,
							Optional: true,
							Computed: true,
						},
						"link_down": {
							Type:     schema.TypeBool,
							Optional: true,
							Default:  false,
						},
					},
				},
			},
			"smbios": {
				Type:     schema.TypeList,
				Optional: true,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"family": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"manufacturer": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"product": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"serial": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"sku": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"uuid": {
							Type:     schema.TypeString,
							Optional: true,
							Computed: true,
						},
						"version": {
							Type:     schema.TypeString,
							Optional: true,
						},
					},
				},
			},
			"unused_disk": {
				Type:     schema.TypeList,
				Computed: true,
				// Optional:      true,
				Description: "Record unused disks in proxmox. This is intended to be read-only for now.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"storage": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"slot": {
							Type:     schema.TypeInt,
							Computed: true,
						},
						"file": {
							Type:     schema.TypeString,
							Computed: true,
						},
					},
				},
			},
			"hostpci": {
				Type:     schema.TypeList,
				Optional: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"host": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"rombar": {
							Type:     schema.TypeInt,
							Optional: true,
						},
						"pcie": {
							Type:     schema.TypeInt,
							Optional: true,
						},
					},
				},
			},
			"efidisk": {
				Type:     schema.TypeList,
				Optional: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"storage": {
							Type:     schema.TypeString,
							Required: true,
							ForceNew: true,
						},
						"efitype": {
							Type:     schema.TypeString,
							Optional: true,
							Default:  "4m",
							ValidateFunc: validation.StringInSlice([]string{
								"2m",
								"4m",
							}, false),
							ForceNew: true,
						},
					},
				},
			},
			"disks": {
				Type:     schema.TypeList,
				Optional: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"ide": {
							Type:     schema.TypeList,
							Optional: true,
							MaxItems: 1,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"ide0": schema_Ide("ide0"),
									"ide1": schema_Ide("ide1"),
									"ide2": schema_Ide("ide2"),
									// ide3 reserved for cloudinit
								},
							},
						},
						"sata": {
							Type:     schema.TypeList,
							Optional: true,
							MaxItems: 1,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"sata0": schema_Sata("sata0"),
									"sata1": schema_Sata("sata1"),
									"sata2": schema_Sata("sata2"),
									"sata3": schema_Sata("sata3"),
									"sata4": schema_Sata("sata4"),
									"sata5": schema_Sata("sata5"),
								},
							},
						},
						"scsi": {
							Type:     schema.TypeList,
							Optional: true,
							MaxItems: 1,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"scsi0":  schema_Scsi("scsi0"),
									"scsi1":  schema_Scsi("scsi1"),
									"scsi2":  schema_Scsi("scsi2"),
									"scsi3":  schema_Scsi("scsi3"),
									"scsi4":  schema_Scsi("scsi4"),
									"scsi5":  schema_Scsi("scsi5"),
									"scsi6":  schema_Scsi("scsi6"),
									"scsi7":  schema_Scsi("scsi7"),
									"scsi8":  schema_Scsi("scsi8"),
									"scsi9":  schema_Scsi("scsi9"),
									"scsi10": schema_Scsi("scsi10"),
									"scsi11": schema_Scsi("scsi11"),
									"scsi12": schema_Scsi("scsi12"),
									"scsi13": schema_Scsi("scsi13"),
									"scsi14": schema_Scsi("scsi14"),
									"scsi15": schema_Scsi("scsi15"),
									"scsi16": schema_Scsi("scsi16"),
									"scsi17": schema_Scsi("scsi17"),
									"scsi18": schema_Scsi("scsi18"),
									"scsi19": schema_Scsi("scsi19"),
									"scsi20": schema_Scsi("scsi20"),
									"scsi21": schema_Scsi("scsi21"),
									"scsi22": schema_Scsi("scsi22"),
									"scsi23": schema_Scsi("scsi23"),
									"scsi24": schema_Scsi("scsi24"),
									"scsi25": schema_Scsi("scsi25"),
									"scsi26": schema_Scsi("scsi26"),
									"scsi27": schema_Scsi("scsi27"),
									"scsi28": schema_Scsi("scsi28"),
									"scsi29": schema_Scsi("scsi29"),
									"scsi30": schema_Scsi("scsi30"),
								},
							},
						},
						"virtio": {
							Type:     schema.TypeList,
							Optional: true,
							MaxItems: 1,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"virtio0":  schema_Virtio("virtio0"),
									"virtio1":  schema_Virtio("virtio1"),
									"virtio2":  schema_Virtio("virtio2"),
									"virtio3":  schema_Virtio("virtio3"),
									"virtio4":  schema_Virtio("virtio4"),
									"virtio5":  schema_Virtio("virtio5"),
									"virtio6":  schema_Virtio("virtio6"),
									"virtio7":  schema_Virtio("virtio7"),
									"virtio8":  schema_Virtio("virtio8"),
									"virtio9":  schema_Virtio("virtio9"),
									"virtio10": schema_Virtio("virtio10"),
									"virtio11": schema_Virtio("virtio11"),
									"virtio12": schema_Virtio("virtio12"),
									"virtio13": schema_Virtio("virtio13"),
									"virtio14": schema_Virtio("virtio14"),
									"virtio15": schema_Virtio("virtio15"),
								},
							},
						},
					},
				},
			},
			// Other
			"serial": {
				Type:     schema.TypeSet,
				Optional: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"id": {
							Type:     schema.TypeInt,
							Required: true,
						},
						"type": {
							Type:     schema.TypeString,
							Required: true,
						},
					},
				},
			},
			"usb": {
				Type:     schema.TypeList,
				Optional: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"host": {
							Type:     schema.TypeString,
							Required: true,
						},
						"usb3": {
							Type:     schema.TypeBool,
							Optional: true,
						},
					},
				},
			},
			"os_type": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"os_network_config": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					return strings.TrimSpace(old) == strings.TrimSpace(new)
				},
			},
			"ssh_forward_ip": {
				Type:         schema.TypeString,
				Optional:     true,
				Description:  "Use to pass instance ip address, redundant",
				ValidateFunc: validation.IsIPv4Address,
			},
			"ssh_user": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},
			"ssh_private_key": {
				Type:      schema.TypeString,
				Optional:  true,
				Sensitive: true,
				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					return strings.TrimSpace(old) == strings.TrimSpace(new)
				},
			},
			"force_create": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},
			"clone_wait": {
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     10,
				Description: "Value in second to wait after a VM has been cloned, useful if system is not fast or during I/O intensive parallel terraform tasks",
			},
			"additional_wait": {
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     5,
				Description: "Value in second to wait after some operations, useful if system is not fast or during I/O intensive parallel terraform tasks",
			},
			"ci_wait": { // how long to wait before provision
				Type:     schema.TypeInt,
				Optional: true,
				Default:  30,
				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					if old == "" {
						return true // old empty ok
					}
					return strings.TrimSpace(old) == strings.TrimSpace(new)
				},
			},
			"ciuser": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"cipassword": {
				Type:      schema.TypeString,
				Optional:  true,
				Sensitive: true,
				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					return new == "**********"
					// if new == "**********" {
					// 	return true // api returns asterisks instead of password so can't diff
					// }
					// return false
				},
			},
			"cicustom": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},
			"searchdomain": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true, // could be pre-existing if we clone from a template with it defined
			},
			"nameserver": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true, // could be pre-existing if we clone from a template with it defined
			},
			"sshkeys": {
				Type:     schema.TypeString,
				Optional: true,
				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					return strings.TrimSpace(old) == strings.TrimSpace(new)
				},
			},
			"ipconfig0": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"ipconfig1": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"ipconfig2": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"ipconfig3": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"ipconfig4": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"ipconfig5": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"ipconfig6": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"ipconfig7": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"ipconfig8": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"ipconfig9": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"ipconfig10": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"ipconfig11": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"ipconfig12": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"ipconfig13": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"ipconfig14": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"ipconfig15": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"pool": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"ssh_host": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"ssh_port": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"force_recreate_on_change_of": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},
			"reboot_required": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Internal variable, true if any of the modified parameters requires a reboot to take effect.",
			},
			"default_ipv4_address": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Use to track vm ipv4 address",
			},
			"define_connection_info": { // by default define SSH for provisioner info
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "By default define SSH for provisioner info",
			},
			"automatic_reboot": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "Automatically reboot the VM if any of the modified parameters requires a reboot to take effect.",
			},
			"linked_vmid": {
				Type:     schema.TypeInt,
				Computed: true,
			},
		},
		Timeouts: resourceTimeouts(),
	}
	return thisResource
}

func resourceTimeouts() *schema.ResourceTimeout {
	return &schema.ResourceTimeout{
		Create:  schema.DefaultTimeout(20 * time.Minute),
		Read:    schema.DefaultTimeout(20 * time.Minute),
		Update:  schema.DefaultTimeout(20 * time.Minute),
		Delete:  schema.DefaultTimeout(20 * time.Minute),
		Default: schema.DefaultTimeout(20 * time.Minute),
	}
}
