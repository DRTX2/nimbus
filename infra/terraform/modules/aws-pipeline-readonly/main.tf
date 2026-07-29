data "aws_iam_policy_document" "trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "AWS"
      identifiers = [var.nimbus_principal_arn]
    }
    condition {
      test     = "StringEquals"
      variable = "sts:ExternalId"
      values   = [var.external_id]
    }
  }
}

resource "aws_iam_role" "nimbus" {
  name_prefix        = "nimbus-pipeline-readonly-"
  assume_role_policy = data.aws_iam_policy_document.trust.json
}

data "aws_iam_policy_document" "readonly" {
  statement {
    sid       = "ReadBoundedCloudWatchMetrics"
    actions   = ["cloudwatch:GetMetricData"]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "readonly" {
  role   = aws_iam_role.nimbus.id
  policy = data.aws_iam_policy_document.readonly.json
}
