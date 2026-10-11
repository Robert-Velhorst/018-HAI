INSERT INTO public.ambient_needs (
    id,
    key,
    name,
    description,
    current_level,
    target_level,
    priority_weight,
    enabled,
    notes,
    created_at,
    updated_at
) VALUES
    (
        '00000000-0000-4000-8000-000000000001',
        'physiological',
        'Health and capacity',
        'Protect time, energy, rest, food, housing basics, and sustainable workload.',
        50, 75, 90, true, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
    ),
    (
        '00000000-0000-4000-8000-000000000002',
        'safety',
        'Safety and stability',
        'Reduce legal, financial, account, deadline, security, and operational risk.',
        50, 85, 100, true, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
    ),
    (
        '00000000-0000-4000-8000-000000000003',
        'belonging',
        'Relationships and belonging',
        'Maintain important relationships, commitments, replies, and collaboration.',
        50, 75, 70, true, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
    ),
    (
        '00000000-0000-4000-8000-000000000004',
        'esteem',
        'Reputation and capability',
        'Improve reliability, professional standing, skills, and completed commitments.',
        50, 80, 65, true, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
    ),
    (
        '00000000-0000-4000-8000-000000000005',
        'growth',
        'Growth and self-direction',
        'Advance meaningful projects, learning, creativity, and long-term agency.',
        50, 85, 60, true, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
    )
ON CONFLICT (key) DO NOTHING;
