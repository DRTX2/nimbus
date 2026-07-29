variable "nimbus_principal_arn" {
  description = "IAM principal used by the Nimbus runtime to assume this role."
  type        = string
}

variable "external_id" {
  description = "Random customer-specific external ID shared out of band."
  type        = string
  sensitive   = true
}
