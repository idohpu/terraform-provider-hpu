terraform {
    required_providers {
        hpu = {
            source  = "local.providers/local/hpu"
            version = "1.0.0"
        }
    }
}

provider "hpu" {
    pm_tls_insecure = true
    pm_api_url = "https://ido-proxmox-pa1.hpu.edu:8006/api2/json"
    pm_api_token_id = ""
    pm_api_token_secret = ""
}

resource "hpu_proxmox_vm" "vm1" {
    name = "something"
}