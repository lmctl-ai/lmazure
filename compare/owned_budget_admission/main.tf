terraform {
  required_providers {
    azurerm = { source = "hashicorp/azurerm", version = "=5.10.0" }
  }
}
provider "azurerm" {
  features {}
  subscription_id                 = var.subscription_id
  resource_provider_registrations = "none"
  use_oidc                        = false
  use_cli                         = true
  use_msi                         = false
  storage_use_azuread             = true
}
variable "subscription_id" {
  type      = string
  sensitive = true
}
variable "storage_account_id" {
  type      = string
  default   = ""
  sensitive = true
}
variable "run_id" {
  type    = string
  default = "0123456789ab"
  validation {
    condition     = can(regex("^[0-9a-f]{12}$", var.run_id))
    error_message = "run_id must be twelve lowercase hexadecimal characters"
  }
}
variable "cost_cap_usd" {
  type    = number
  default = 0.0002
  validation {
    condition     = var.cost_cap_usd >= 0 && var.cost_cap_usd <= 0.25
    error_message = "example cap must be between zero and the 0.25 USD hard ceiling"
  }
}
resource "terraform_data" "admission" {
  input = 0.0003
  lifecycle {
    precondition {
      condition     = var.cost_cap_usd >= 0.0003
      error_message = "estimated provider allowance exceeds the example cap"
    }
  }
}
output "comparison" {
  value = jsonencode({ resources_created = [], outputs = { inventory = [] } })
}
