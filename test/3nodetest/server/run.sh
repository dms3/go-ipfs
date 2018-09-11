# must be connected to bootstrap node
dms3fs bootstrap add /ip4/$BOOTSTRAP_PORT_4011_TCP_ADDR/tcp/$BOOTSTRAP_PORT_4011_TCP_PORT/dms3fs/QmNXuBh8HFsWq68Fid8dMbGNQTh7eG6hV9rr1fQyfmfomE
dms3fs bootstrap # list bootstrap nodes for debugging

# wait for daemon to start/bootstrap
# alternatively use dms3fs swarm connect
echo "3nodetest> starting server daemon"

# run daemon in debug mode to collect profiling data
dms3fs daemon --debug &
sleep 3
# TODO instead of bootrapping: dms3fs swarm connect /ip4/$BOOTSTRAP_PORT_4011_TCP_ADDR/tcp/$BOOTSTRAP_PORT_4011_TCP_PORT/dms3fs/QmNXuBh8HFsWq68Fid8dMbGNQTh7eG6hV9rr1fQyfmfomE

# change dir before running add commands so dms3fs client profiling data doesn't
# overwrite the daemon profiling data
cd /tmp

# must mount this volume from data container
dms3fs add -q /data/filetiny > tmptiny
mv tmptiny /data/idtiny
echo "3nodetest> added tiny file. hash is" $(cat /data/idtiny)

dms3fs add -q /data/filerand > tmprand
mv tmprand /data/idrand
echo "3nodetest> added rand file. hash is" $(cat /data/idrand)

# allow ample time for the client to pull the data
sleep 10000000
