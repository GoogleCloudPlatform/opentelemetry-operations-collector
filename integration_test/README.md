# Integration Testing

Integration tests are implemented as Louhi flows that run on each commit to
master.

## Setup

You will need a GCP project to run VMs in. This is referred to as `${PROJECT}` in
the following instructions.

The project needs sufficient quota to run many tests in parallel. It also needs
a firewall that allows connections over port 22 for ssh. It is recommended for
Googlers to use our prebuilt testing project. Ask a teammate (e.g. martijnvs@)
for the project ID.

You will also need a GCS bucket that is used to transfer files onto the
testing VMs. This is referred to as `${TRANSFERS_BUCKET}`. For Googlers,
`stackdriver-test-143416-untrusted-file-transfers` is recommended.

You will need `gcloud` to be installed. Run `gcloud auth login` to set up `gcloud`
authentication (if you haven't done that already).

To give the tests credentials to be able to access Google APIs as you,
run the following command and do what it says (it may ask you to run
a command on a separate machine if your main machine doesn't have the
ability to open a browser window):

```
gcloud --billing-project="${PROJECT}" auth application-default login
```

Once these steps are complete, you should be able to run the tests locally.

## Smoke Test

See instructions in smoke\_test.go for specifics on running that test.
