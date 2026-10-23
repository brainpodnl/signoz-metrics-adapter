# DEV-1740: static trust stores do not receive Firefox OneCRL updates.
# https://blog.mozilla.org/security/2026/10/20/protecting-firefox-users-following-the-compromise-of-emudhra-intermediate-certificates/
# https://lists.debian.org/debian-lts-announce/2026/10/msg00039.html
# Remove the two trust anchors before generating every CA output. A blacklist
# alone keeps OpenSSL AUX-rejected certificates, which not all clients honor.
final: prev: {
  cacert = prev.cacert.overrideAttrs (old: {
    postPatch = (old.postPatch or "") + ''
      ${prev.buildPackages.python3}/bin/python3 ${./remove-distrusted-ca.py} certdata.txt
    '';
  });
}
