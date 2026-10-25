variable "run_id" {
  type = string
  default = "0123456789ab"
}
resource "terraform_data" "intent" {
  input = {
    resources_created = []
    outputs = {inventory = [{"name": "lmazure-8", "location": "westus2", "tagged": true, "type": "Microsoft.Insights/components"}, {"name": "lmazure-18", "location": "westeurope", "tagged": false, "type": "Microsoft.Insights/metricAlerts"}]}
  }
}
output "comparison" {
  value = jsonencode(terraform_data.intent.output)
}
