# Add fake installation-id to the config file.
sudo mkdir ~/.keploy-config
sudo cat ~/.keploy-config/installation-id.yaml
echo "ObjectID('123456789')" > ~/.keploy-config/installation-id.yaml