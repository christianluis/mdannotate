class Mda < Formula
  desc "Markdown im Browser bearbeiten und jede Aenderung mit Randmarken versehen"
  homepage "https://github.com/christianluis/mdannotate"
  head "https://github.com/christianluis/mdannotate.git", branch: "main"

  # Fuer eine feste Version: Tag setzen, Tarball hochladen und die beiden
  # Zeilen einkommentieren. Den Pruefwert liefert:
  #   curl -sL <url> | shasum -a 256
  #
  # url "https://github.com/christianluis/mdannotate/archive/refs/tags/v0.1.0.tar.gz"
  # sha256 "hier den Pruefwert eintragen"

  depends_on "go" => :build

  def install
    ldflags = "-s -w -X main.version=#{version}"
    system "go", "build", *std_go_args(ldflags: ldflags), "./cmd/mda"
  end

  test do
    assert_match "mda", shell_output("#{bin}/mda -version")
  end
end
