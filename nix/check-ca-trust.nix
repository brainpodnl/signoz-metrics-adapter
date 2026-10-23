{
  runCommand,
  cacert,
  dockerTools,
  originalCacert,
  python3,
  openssl
}:
runCommand "check-ca-distrust"
  {
    nativeBuildInputs = [python3 openssl];
  }
  ''
    python3 ${./check-ca-trust.py} \
      ${originalCacert}/etc/ssl/certs/ca-bundle.crt \
      ${cacert}/etc/ssl/certs/ca-bundle.crt \
      ${dockerTools.caCertificates}/etc/ssl/certs/ca-certificates.crt \
      ${dockerTools.caCertificates}/etc/pki/tls/certs/ca-bundle.crt
    python3 ${./check-ca-trust.py} \
      ${originalCacert}/etc/ssl/certs/ca-no-trust-rules-bundle.crt \
      ${cacert}/etc/ssl/certs/ca-no-trust-rules-bundle.crt
    touch "$out"
  ''

