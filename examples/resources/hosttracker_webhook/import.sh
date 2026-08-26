# A webhook is imported by its id, which the web app shows under
# Integrations and `hosttracker_webhooks` publishes as `ids`.
terraform import hosttracker_webhook.ops 0c3c7b07-cecb-43dd-9b76-8516d3b9c771

# The signing secret does NOT come with it: the API publishes a secret's
# value only in the answer that mints it. Set `rotate_secret` on the
# adopted resource and apply to mint one Terraform can hold.
