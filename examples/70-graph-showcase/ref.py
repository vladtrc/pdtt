from manim import *
import numpy as np


class GraphShowcase(MovingCameraScene):
    def construct(self):
        axes = Axes(
            x_range=[-4, 4, 1],
            y_range=[-2.5, 2.5, 1],
            x_length=10,
            y_length=6,
            tips=True,
        ).shift(DOWN * 0.2)
        grid = NumberPlane(
            x_range=[-4, 4, 1],
            y_range=[-2.5, 2.5, 1],
            x_length=10,
            y_length=6,
            background_line_style={"stroke_color": BLUE_D, "stroke_opacity": 0.45},
        ).shift(DOWN * 0.2)

        title = Text("broadcast [*] samples").scale(0.55).to_corner(UL)
        note = Text("sample[*].at -> live curve", color=GREEN).scale(0.42).next_to(title, DOWN)

        phase = ValueTracker(0)
        mix = ValueTracker(0)
        xs = [-3.6 + i * 0.9 for i in range(9)]

        wave = always_redraw(
            lambda: axes.plot(lambda x: 1.35 * np.sin(1.4 * x + phase.get_value()), color=YELLOW)
        )
        bowl = always_redraw(
            lambda: axes.plot(lambda x: 0.22 * x * x - 1.2 + 0.45 * np.sin(phase.get_value()), color=BLUE)
        )
        blend = always_redraw(
            lambda: axes.plot(
                lambda x: (1 - mix.get_value()) * (1.35 * np.sin(1.4 * x + phase.get_value()))
                + mix.get_value() * (0.22 * x * x - 1.2 + 0.45 * np.sin(phase.get_value())),
                color=PINK,
            )
        )

        samples = VGroup(*[
            always_redraw(lambda x=x: Dot(axes.c2p(x, 1.35 * np.sin(1.4 * x + phase.get_value())), radius=0.08))
            for x in xs
        ])
        probes = VGroup(*[
            always_redraw(lambda x=x, d=d: Arrow(axes.c2p(x, -2.25), d.get_center(), buff=0, color=GREEN))
            for x, d in zip(xs, samples)
        ])
        phase_label = always_redraw(
            lambda: Text("phase", color=YELLOW).scale(0.35).move_to(axes.c2p(-3.6 + 7.2 * phase.get_value() / TAU, 2.2))
        )
        mix_label = always_redraw(
            lambda: Text("mix", color=PINK).scale(0.35).move_to(axes.c2p(2.9, -2.1 + 4.0 * mix.get_value()))
        )

        self.play(Create(grid), FadeIn(axes), FadeIn(title), run_time=1.5)
        self.play(Create(wave), FadeIn(note), run_time=1.5, rate_func=linear)
        self.play(LaggedStart(*[FadeIn(s, scale=1.7) for s in samples], lag_ratio=0.08), Create(probes), run_time=1.5)
        self.add(phase_label)
        self.play(phase.animate.set_value(TAU), run_time=4, rate_func=linear)
        self.play(Create(bowl), Create(blend), run_time=1.5)
        self.add(mix_label)
        self.play(mix.animate.set_value(1), phase.animate.set_value(1.35 * TAU), run_time=4)
        self.play(self.camera.frame.animate.set(width=6.2).move_to(samples[5]), samples.animate.scale(1.4), probes.animate.set_color(YELLOW), run_time=2)
        self.play(phase.animate.set_value(1.75 * TAU), self.camera.frame.animate.move_to(samples[5]), run_time=2, rate_func=linear)
        self.play(self.camera.frame.animate.set(width=14.2).move_to(ORIGIN), samples.animate.scale(1 / 1.4), probes.animate.set_opacity(0.45), run_time=1.5)
        self.play(FadeOut(title), FadeOut(note), FadeOut(phase_label), FadeOut(mix_label), run_time=1)
