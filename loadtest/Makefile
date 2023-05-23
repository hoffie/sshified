.PHONY = prepare cleanup run-webserver run-sshified run-loadtest
USER = sshified-test

prepare:
		mkdir -p conf/
		id $(USER) || sudo useradd $(USER) -s /bin/false -m
		ssh-keygen -f conf/id_rsa -N ""
		sudo -u $(USER) mkdir -p /home/$(USER)/.ssh/
		sudo -u $(USER) tee /home/$(USER)/.ssh/authorized_keys < conf/id_rsa.pub
		sudo -u $(USER) chmod 700 /home/$(USER)/.ssh/
		sudo -u $(USER) chmod 600 /home/$(USER)/.ssh/authorized_keys
		ssh-keyscan 127.0.0.1 > conf/known_hosts

cleanup:
	rm -rf conf/
	sudo pkill -u $(USER) || true
	sudo -u $(USER) rm -rf /home/$(USER) || true
	sudo userdel $(USER) || true


run-webserver:
	go run server.go

run-sshified:
	../sshified --proxy.listen-addr 127.0.0.1:8888 --ssh.user $(USER) --ssh.key-file conf/id_rsa --ssh.known-hosts-file conf/known_hosts

run-loadtest:
	ab -n20000 -c500 -X 127.0.0.1:8888 http://127.0.0.1:8080/bar
