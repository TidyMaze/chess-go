import depth_probe

UCI_OUTPUT = """id name chess-go
uciok
readyok
info depth 1 score cp 12 nodes 40 nps 400000 time 0
info depth 7 score cp 35 nodes 61234 nps 1200000 time 51
info depth 8 score cp -20 nodes 120444 nps 1180000 time 102
bestmove e2e4
"""


def test_the_deepest_completed_iteration_and_the_move_are_read_back() -> None:
    r = depth_probe.parse_search(UCI_OUTPUT)
    assert (r.depth, r.nodes, r.move, r.score_cp) == (8, 120444, "e2e4", -20)


def test_a_search_that_printed_no_iteration_reports_depth_zero() -> None:
    r = depth_probe.parse_search("uciok\nreadyok\nbestmove a2a3\n")
    assert (r.depth, r.nodes, r.move) == (0, 0, "a2a3")


def test_a_mate_score_is_kept_as_a_large_value() -> None:
    r = depth_probe.parse_search("info depth 5 score mate 3 nodes 999 time 10\nbestmove d1h5\n")
    assert r.score_cp is not None and r.score_cp > 10000 and r.depth == 5
