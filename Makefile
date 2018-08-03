SSH=gcloud compute ssh namegame --project fs-playpen --zone us-central1-b
SCP=gcloud compute scp --project fs-playpen --zone us-central1-b

mac:
	go build

linux:
	GOOS=linux GOARCH=amd64 go build -o namegame.linux

push: linux
	$(SCP) namegame.linux namegame:

restart: push
	$(SSH) --command 'pkill namegame; mv ./namegame.linux ./namegame; nohup ./namegame > log.out 2>&1 &'


