data "hosttracker_status_pages" "all" {}

# The pages currently reporting trouble.
output "pages_with_open_incidents" {
  value = {
    for page in data.hosttracker_status_pages.all.status_pages :
    page.slug => page.unresolved_incidents if page.unresolved_incidents > 0
  }
}
