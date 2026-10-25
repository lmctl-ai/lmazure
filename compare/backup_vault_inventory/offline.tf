variable "run_id" {
  type = string
  default = "0123456789ab"
}
resource "terraform_data" "intent" {
  input = {
    resources_created = []
    outputs = {inventory = [{"name": "lmazure-23", "location": "westus2", "tagged": true, "type": "Microsoft.RecoveryServices/vaults"}]}
  }
}
output "comparison" {
  value = jsonencode(terraform_data.intent.output)
}
