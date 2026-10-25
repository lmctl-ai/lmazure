variable "run_id" {
  type = string
  default = "0123456789ab"
}
resource "terraform_data" "intent" {
  input = {
    resources_created = []
    outputs = {inventory = [{"name": "lmazure-15", "location": "westeurope", "tagged": false, "type": "Microsoft.Network/publicIPAddresses"}]}
  }
}
output "comparison" {
  value = jsonencode(terraform_data.intent.output)
}
