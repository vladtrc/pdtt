"""Manim reference for the Duffing phase-portrait morph (best-effort)."""

from manim import *
import numpy as np


class PhasePortrait(Scene):
    def construct(self):
        a = ValueTracker(0.0)
        energies = [0.35, 0.7, 1.05, 1.4, 1.75, 2.1]
        colors = [TEAL, BLUE, GREEN, YELLOW, PINK, RED]

        axes = Axes(
            x_range=[-2.6, 2.6, 0.5],
            y_range=[-2.2, 2.2, 0.5],
            x_length=10,
            y_length=7,
        ).shift(DOWN * 0.15)
        grid = NumberPlane(
            x_range=[-2.6, 2.6, 0.5],
            y_range=[-2.2, 2.2, 0.5],
            x_length=10,
            y_length=7,
            background_line_style={"stroke_color": BLUE_D, "stroke_opacity": 0.45},
        ).shift(DOWN * 0.15)

        def orbit(e, sign):
            def f(x):
                disc = 2 * e - x * x + (a.get_value() / 2) * x**4
                if disc <= 0.001:
                    return np.nan
                return sign * np.sqrt(disc)

            return axes.plot(f, x_range=[-2.6, 2.6], color=WHITE)

        curves = VGroup()
        for e, col in zip(energies, colors):
            hi = always_redraw(lambda e=e, col=col: axes.plot(
                lambda x, e=e: np.sqrt(max(2 * e - x * x + (a.get_value() / 2) * x**4, 0)),
                x_range=[-2.6, 2.6], color=col))
            lo = always_redraw(lambda e=e, col=col: axes.plot(
                lambda x, e=e: -np.sqrt(max(2 * e - x * x + (a.get_value() / 2) * x**4, 0)),
                x_range=[-2.6, 2.6], color=col))
            curves.add(hi, lo)

        title = MathTex(r"x'' + x = a x^3").to_corner(UL)
        a_label = always_redraw(lambda: MathTex(f"a = {a.get_value():.3f}").next_to(title, DOWN))

        self.play(Create(grid), FadeIn(axes), Write(title), run_time=1.4)
        self.play(FadeIn(a_label), run_time=0.5)
        self.play(LaggedStart(*[Create(c) for c in curves], lag_ratio=0.05), run_time=2.2)
        self.wait(0.5)
        self.play(a.animate.set_value(0.22), run_time=10, rate_func=linear)
        self.wait(2)
        self.play(FadeOut(grid), FadeOut(axes), FadeOut(curves), FadeOut(title), FadeOut(a_label))
