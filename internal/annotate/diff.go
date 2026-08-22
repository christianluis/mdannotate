package annotate

// Zeilenweiser Diff ueber die laengste gemeinsame Teilfolge. Markdown-Dateien
// sind klein; gemeinsamer Anfang und gemeinsames Ende werden vorher abgeschnitten,
// so dass die quadratische Tabelle fast immer winzig bleibt.

type opKind byte

const (
	opEqual opKind = '='
	opDel   opKind = '-'
	opIns   opKind = '+'
)

// op beschreibt einen Schritt im Editierskript. a ist die Zeile in der alten,
// b die Zeile in der neuen Fassung. Bei opDel zeigt b auf die Stelle, an der
// die geloeschte Zeile gestanden haette.
type op struct {
	kind opKind
	a, b int
}

// maxCells begrenzt die DP-Tabelle. Darueber wird der Rest als ein einziger
// Block ersetzt behandelt, was hoechstens eine groessere Annotation ergibt.
const maxCells = 4_000_000

func diffLines(a, b []string) []op {
	n, m := len(a), len(b)

	pre := 0
	for pre < n && pre < m && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < n-pre && suf < m-pre && a[n-1-suf] == b[m-1-suf] {
		suf++
	}

	ops := make([]op, 0, n+m)
	for i := 0; i < pre; i++ {
		ops = append(ops, op{opEqual, i, i})
	}
	ops = append(ops, lcs(a[pre:n-suf], b[pre:m-suf], pre, pre)...)
	for i := 0; i < suf; i++ {
		ops = append(ops, op{opEqual, n - suf + i, m - suf + i})
	}
	return ops
}

func lcs(a, b []string, offA, offB int) []op {
	n, m := len(a), len(b)
	if n == 0 || m == 0 || n*m > maxCells {
		ops := make([]op, 0, n+m)
		for i := 0; i < n; i++ {
			ops = append(ops, op{opDel, offA + i, offB})
		}
		for j := 0; j < m; j++ {
			ops = append(ops, op{opIns, offA + n, offB + j})
		}
		return ops
	}

	dp := make([]int32, (n+1)*(m+1))
	at := func(i, j int) int { return i*(m+1) + j }
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				dp[at(i, j)] = dp[at(i+1, j+1)] + 1
			case dp[at(i+1, j)] >= dp[at(i, j+1)]:
				dp[at(i, j)] = dp[at(i+1, j)]
			default:
				dp[at(i, j)] = dp[at(i, j+1)]
			}
		}
	}

	ops := make([]op, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{opEqual, offA + i, offB + j})
			i++
			j++
		case dp[at(i+1, j)] >= dp[at(i, j+1)]:
			ops = append(ops, op{opDel, offA + i, offB + j})
			i++
		default:
			ops = append(ops, op{opIns, offA + i, offB + j})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{opDel, offA + i, offB + j})
	}
	for ; j < m; j++ {
		ops = append(ops, op{opIns, offA + i, offB + j})
	}
	return ops
}
