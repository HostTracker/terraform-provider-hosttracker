# A subscription has no id of its own: the monitor-and-contact pair IS the
# identity, so it is imported as "<monitor id>/<contact id>".
terraform import hosttracker_report_subscription.marketing_reports \
  4e49d7a2-4ab5-45e2-b9f8-1d59f505ad45/0c3c7b07-cecb-43dd-9b76-8516d3b9c771
