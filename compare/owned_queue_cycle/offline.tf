variable "run_id" {
  type = string
  default = "0123456789ab"
}
resource "terraform_data" "intent" {
  input = {
    resources_created = [{ type = "azurerm_storage_queue", name = "lmazure-${var.run_id}-gallery", tags = {lmazure_test = var.run_id}, metadata_sha256 = sha256(jsonencode({"gallery" = "true", "lmazure_test" = var.run_id})) }]
    outputs = {inventory = []}
  }
}
output "comparison" {
  value = jsonencode(terraform_data.intent.output)
}
