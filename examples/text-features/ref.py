from manim import *


class TextFeatures(Scene):
    def construct(self):
        # manim reference for pdtt's text toolkit — two ways to emphasise a span:
        #   LEFT  column — transient highlights that light up then settle back to
        #                  rest (Indicate / ShowPassingFlash / Wiggle).
        #   RIGHT column — persistent edits that set the value and HOLD it.
        # pdtt renders the same ideas NATIVELY through typst (no LaTeX).
        title = Text("Two ways to emphasise").scale(0.8).to_edge(UP)
        cap_l = Text("| modifier |  — flashes, then fades", color=GRAY).scale(0.4)
        cap_r = Text("->  — sets it, and stays", color=GRAY).scale(0.4)
        cap_l.move_to(3 * LEFT + 1.7 * UP)
        cap_r.move_to(3 * RIGHT + 1.7 * UP)

        words = ["colour", "strike", "underline", "enlarge", "wiggle"]
        left = VGroup(*[Text(w) for w in words]).arrange(DOWN).scale(0.7)
        right = VGroup(*[Text(w) for w in words]).arrange(DOWN).scale(0.7)
        left.move_to(3 * LEFT + 0.55 * DOWN)
        right.move_to(3 * RIGHT + 0.55 * DOWN)

        self.play(FadeIn(title), FadeIn(cap_l), FadeIn(cap_r), run_time=0.8)
        self.play(Write(left), Write(right), run_time=1.4)

        l_colour, l_strike, l_underline, l_enlarge, l_wiggle = left
        r_colour, r_strike, r_underline, r_enlarge, _ = right

        # colour: left flashes yellow and returns; right goes yellow and holds
        self.play(
            Indicate(l_colour, color=YELLOW),
            r_colour.animate.set_color(YELLOW),
            run_time=1.1,
        )

        # strike: left sweeps a rule in and out; right keeps it struck
        r_strike_line = Line(r_strike.get_left(), r_strike.get_right())
        self.play(
            ShowPassingFlash(Line(l_strike.get_left(), l_strike.get_right())),
            Create(r_strike_line),
            run_time=1.1,
        )

        # underline: same contrast, one line down
        self.play(
            ShowPassingFlash(Underline(l_underline)),
            Create(Underline(r_underline)),
            run_time=1.1,
        )

        # enlarge: left swells then settles; right stays big
        self.play(
            Indicate(l_enlarge, scale_factor=1.5, color=WHITE),
            r_enlarge.animate.scale(1.6),
            run_time=1.1,
        )

        # wiggle: modifier only — a self-contained shake with no "stays" form
        self.play(Wiggle(l_wiggle), run_time=1.1)

        # the `->` value is a live number — scrub the strike rule to show it is a
        # continuous edit, not an on/off flag
        self.play(r_strike_line.animate.scale(0.7), run_time=0.5)
        self.play(r_strike_line.animate.scale(0.2 / 0.7), run_time=0.5)
        self.play(r_strike_line.animate.scale(1 / 0.2), run_time=0.6)

        self.play(Unwrite(left), Unwrite(right), run_time=1.6)
