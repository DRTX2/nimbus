variable "aws_region" {
  description = "AWS region for the sandbox."
  type        = string
  default     = "us-east-1"
}

variable "container_image" {
  description = "Public Nimbus container image pinned to an immutable SHA tag."
  type        = string
}

variable "config_parameter_name" {
  description = "Existing SecureString SSM parameter containing the runtime env file."
  type        = string
}

variable "instance_type" {
  description = "Small EC2 type. Eligibility and price depend on account and region."
  type        = string
  default     = "t3.micro"
}

variable "budget_email" {
  description = "Email for the optional AWS monthly cost budget; blank disables it."
  type        = string
  default     = ""
}

variable "monthly_budget_usd" {
  description = "Monthly cost threshold in USD."
  type        = number
  default     = 5
}
