data "aws_partition" "current" {}
data "aws_caller_identity" "current" {}

data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
}

data "aws_ssm_parameter" "al2023_ami" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64"
}

resource "aws_security_group" "nimbus" {
  name_prefix = "nimbus-sandbox-"
  description = "Restricted access to the Nimbus portfolio demo"
  vpc_id      = data.aws_vpc.default.id

  egress {
    description = "Container pulls, AWS APIs, and configured workflow destinations"
    protocol    = "-1"
    from_port   = 0
    to_port     = 0
    cidr_blocks = ["0.0.0.0/0"]
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_iam_role" "instance" {
  name_prefix = "nimbus-sandbox-"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ssm_core" {
  role       = aws_iam_role.instance.name
  policy_arn = "arn:${data.aws_partition.current.partition}:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_role_policy" "runtime_config" {
  name = "read-nimbus-runtime-config"
  role = aws_iam_role.instance.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["ssm:GetParameter"]
      Resource = "arn:${data.aws_partition.current.partition}:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${var.config_parameter_name}"
    }]
  })
}

resource "aws_iam_instance_profile" "nimbus" {
  name_prefix = "nimbus-sandbox-"
  role        = aws_iam_role.instance.name
}

resource "aws_instance" "nimbus" {
  ami                         = data.aws_ssm_parameter.al2023_ami.value
  instance_type               = var.instance_type
  subnet_id                   = sort(data.aws_subnets.default.ids)[0]
  associate_public_ip_address = true
  vpc_security_group_ids      = [aws_security_group.nimbus.id]
  iam_instance_profile        = aws_iam_instance_profile.nimbus.name
  monitoring                  = false

  metadata_options {
    http_endpoint = "enabled"
    http_tokens   = "required"
  }

  root_block_device {
    encrypted   = true
    volume_type = "gp3"
    volume_size = 8
  }

  user_data = templatefile("${path.module}/user_data.tftpl", {
    aws_region            = var.aws_region
    config_parameter_name = var.config_parameter_name
    container_image       = var.container_image
  })

  tags = {
    Name = "nimbus-sandbox"
  }
}

resource "aws_budgets_budget" "sandbox" {
  count        = var.budget_email == "" ? 0 : 1
  name         = "nimbus-sandbox-monthly"
  budget_type  = "COST"
  limit_amount = tostring(var.monthly_budget_usd)
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 80
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [var.budget_email]
  }
}
