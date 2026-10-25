variable "run_id" {
  type = string
  default = "0123456789ab"
}
resource "terraform_data" "intent" {
  input = {
    resources_created = []
    outputs = {inventory = [{"name": "lmazure-1", "location": "eastus", "tagged": true, "type": "Microsoft.Compute/virtualMachines"}]}
  }
}
output "comparison" {
  value = jsonencode(terraform_data.intent.output)
}
