variable "run_id" {
  type = string
  default = "0123456789ab"
}
resource "terraform_data" "intent" {
  input = {
    resources_created = []
    outputs = {inventory = [{"name": "lmazure-2", "location": "westus2", "tagged": true, "type": "Microsoft.Storage/storageAccounts"}, {"name": "default", "location": "westeurope", "tagged": false, "type": "Microsoft.Storage/storageAccounts/queueServices"}, {"name": "lmazure-12", "location": "westeurope", "tagged": false, "type": "Microsoft.Storage/storageAccounts"}]}
  }
}
output "comparison" {
  value = jsonencode(terraform_data.intent.output)
}
