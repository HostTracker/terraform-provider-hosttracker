# A contact created in the web app is adopted by its id, which the
# `hosttracker_contacts` data source publishes as `ids`. A messenger
# contact, which can only be created by registering with the bot, is
# adopted the same way.
terraform import hosttracker_contact.oncall 8e2d4c8b-7a41-4a2b-9d0e-2f3a5c6b7d8e
