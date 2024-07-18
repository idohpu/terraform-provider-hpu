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
    pm_api_token_id = "apiuser@pam!terraform_token_1"
    pm_api_token_secret = "a5cce985-97c1-41ad-a3a3-959a77452de8"
}

resource "hpu_proxmox_vm" "vm1" {
    name = "something"
}