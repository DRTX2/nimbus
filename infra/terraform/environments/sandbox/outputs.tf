output "ssm_port_forward_command" {
  description = "Open a private localhost tunnel to Nimbus; requires the Session Manager plugin."
  value       = "aws ssm start-session --target ${aws_instance.nimbus.id} --document-name AWS-StartPortForwardingSession --parameters '{\"portNumber\":[\"8080\"],\"localPortNumber\":[\"8080\"]}'"
}

output "instance_id" {
  description = "Use this ID with AWS Systems Manager Session Manager; there is no SSH ingress."
  value       = aws_instance.nimbus.id
}
