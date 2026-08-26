# A status page is imported by its id, which the `hosttracker_status_page`
# data source resolves from the slug:
#
#   data "hosttracker_status_page" "acme" { slug = "acme-status" }
#
terraform import hosttracker_status_page.acme 6b1f2a70-3c8e-4d51-9f2a-7c4e5d6b8a90

# The component set comes with it, in display order. Write the components
# out in the configuration to match, or leave the attribute off and manage
# them in the web app.
