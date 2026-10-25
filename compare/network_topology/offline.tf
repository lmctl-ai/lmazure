variable "run_id" {
  type = string
  default = "0123456789ab"
}
resource "terraform_data" "intent" {
  input = {
    resources_created = []
    outputs = {inventory = [{"name": "lmazure-3", "location": "westeurope", "tagged": false, "type": "Microsoft.Network/virtualNetworks"}, {"name": "lmazure-9", "location": "westeurope", "tagged": false, "type": "Microsoft.Network/dnsZones"}, {"name": "lmazure-14", "location": "westus2", "tagged": true, "type": "Microsoft.Network/networkSecurityGroups"}, {"name": "lmazure-15", "location": "westeurope", "tagged": false, "type": "Microsoft.Network/publicIPAddresses"}, {"name": "lmazure-16", "location": "eastus", "tagged": true, "type": "Microsoft.Network/routeTables"}]}
  }
}
output "comparison" {
  value = jsonencode(terraform_data.intent.output)
}
