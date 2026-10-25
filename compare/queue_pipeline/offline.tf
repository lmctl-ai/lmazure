variable "run_id" {
  type = string
  default = "0123456789ab"
}
resource "terraform_data" "intent" {
  input = {
    resources_created = []
    outputs = {inventory = [{"name": "default", "location": "westeurope", "tagged": false, "type": "Microsoft.Storage/storageAccounts/queueServices"}]}
  }
}
output "comparison" {
  value = jsonencode(terraform_data.intent.output)
}
